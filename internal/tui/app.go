package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aidanwolter/ticket/internal/agent"
	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/views"
	"github.com/aidanwolter/ticket/internal/workflow"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hinshun/vt10x"
)

// attachTermCols/Rows must match agent.PTYCols/PTYRows exactly so that cursor-
// positioning sequences from the PTY are interpreted at the correct coordinates.
const attachTermCols = agent.PTYCols
const attachTermRows = agent.PTYRows

// renderTermViewport returns exactly avail lines from the terminal.
//
// Claude Code (Ink-based) does not clear lines below the new render area when
// the UI shrinks, leaving stale content in those rows. We detect this by
// scanning from cursor+footerRows downward for the first blank row: that blank
// row marks the end of active content. Everything from there down is excluded
// from the viewport, preventing stale/duplicate UI elements from being shown.
func renderTermViewport(term vt10x.Terminal, avail int) []string {
	if avail < 1 {
		avail = 1
	}
	cursorRow := term.Cursor().Y
	raw := strings.Split(term.String(), "\n")

	// Claude Code's footer sits ~4 rows below the cursor (separator, status).
	// Start scanning for a blank gap just past that.
	const footerRows = 4
	maxRow := len(raw) - 1
	for i := cursorRow + footerRows; i < len(raw); i++ {
		if strings.TrimSpace(raw[i]) == "" {
			maxRow = i - 1 // stop before the blank gap
			break
		}
	}

	// Viewport: show avail rows ending at maxRow, cursor near the bottom.
	startRow := maxRow - avail + 1
	if startRow < 0 {
		startRow = 0
	}

	result := make([]string, avail)
	for i := 0; i < avail; i++ {
		row := startRow + i
		if row <= maxRow && row < len(raw) {
			result[i] = strings.TrimRight(raw[row], " ")
		}
	}
	return result
}

// agentChunkMsg carries a broadcast chunk from the attach channel.
type agentChunkMsg struct{ data []byte }

// agentDoneMsg is sent when the attach channel closes (session ended).
type agentDoneMsg struct{}

// agentRenderMsg is a debounced snapshot trigger. Each agentChunkMsg schedules
// one 30 ms after the chunk; only the most-recent seq fires a real snapshot.
// This ensures a multi-read PTY redraw (e.g. Ink clearing and redrawing the full
// screen across several 4096-byte reads) is always captured in its final state.
type agentRenderMsg struct{ seq int }

type appTab int

const (
	tabTickets appTab = iota
)

type appScreen int

const (
	screenList appScreen = iota // split-pane: list left, detail right
	screenThreads
	screenNoteModal
	screenReplyModal
	screenNewThreadModal
	screenConfirmDelete
	screenEditDraftMessage
	screenConfirmDispatch
	screenAgentAttach
)

type dbTickMsg struct{}

func tickDB() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return dbTickMsg{} })
}

type App struct {
	store            *store.Store
	launcher         *agent.Launcher
	tab              appTab
	screen           appScreen
	width            int
	height           int
	leftW            int
	rightW           int
	statusMsg        string
	statusErr        bool
	pendingDeleteID  string
	pendingDispatchID string
	showHelp         bool

	// list views (left pane)
	ticketsView *views.TicketsView

	// detail view (right pane)
	ticketDetail *views.TicketDetailView

	// overlay screens
	threadsView     *views.ThreadsView
	noteModal       *views.NoteModal
	replyModal      *views.ReplyModal
	newThreadModal  *views.NewThreadModal
	editDraftModal  *views.EditDraftModal

	// agent attach overlay
	attachFollow     <-chan []byte
	attachUnsub      func()
	attachTerm       vt10x.Terminal // virtual screen fed from PTY output
	attachLines      []string       // rendered snapshot of attachTerm
	attachTicketID   string
	attachSessionID  string
	attachEnded      bool // true once the session's channel closed
	attachRenderSeq  int  // incremented on each chunk; debounces render ticks
}

func New(s *store.Store) *App {
	a := &App{
		store:       s,
		launcher:    agent.NewLauncher(s),
		tab:         tabTickets,
		screen:      screenList,
		width:       80,
		height:      24,
		leftW:       28,
		rightW:      51,
		ticketsView: views.NewTicketsView(s),
	}
	a.loadCurrentDetail()
	return a
}

func (a *App) Init() tea.Cmd {
	return tickDB()
}

func (a *App) bodyHeight() int {
	h := a.height - 3 // tab bar + divider + status
	if h < 1 {
		h = 20
	}
	return h
}

func (a *App) selectedTicketID() string {
	if t := a.ticketsView.SelectedTicket(); t != nil {
		return t.ID
	}
	return ""
}

func (a *App) loadCurrentDetail() {
	a.loadDetailForID(a.selectedTicketID())
}

func (a *App) loadDetailForID(id string) {
	if id == "" {
		a.ticketDetail = nil
		return
	}
	td, err := views.NewTicketDetailView(a.store, id)
	if err != nil {
		a.ticketDetail = nil
		return
	}
	td.SetSize(a.rightW, a.bodyHeight())
	a.ticketDetail = td
}

func (a *App) currentTicketID() string {
	if a.ticketDetail != nil && a.ticketDetail.Ticket() != nil {
		return a.ticketDetail.Ticket().ID
	}
	return ""
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case dbTickMsg:
		a.ticketsView.Refresh()
		if a.ticketDetail != nil {
			_ = a.ticketDetail.Reload()
		}
		if a.threadsView != nil {
			_ = a.threadsView.Reload()
		}
		a.terminateSilentReviewedSessions()
		return a, tickDB()

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.leftW = a.width * 35 / 100
		if a.leftW < 20 {
			a.leftW = 20
		}
		a.rightW = a.width - a.leftW - 1 // -1 for the │ separator
		if a.rightW < 10 {
			a.rightW = 10
		}
		bodyH := a.bodyHeight()
		a.ticketsView.SetSize(a.leftW, bodyH)
		if a.ticketDetail != nil {
			a.ticketDetail.SetSize(a.rightW, bodyH)
		}
		if a.threadsView != nil {
			a.threadsView.SetSize(a.width, a.height)
		}
		if a.replyModal != nil {
			a.replyModal.SetWidth(a.width)
		}
		if a.newThreadModal != nil {
			a.newThreadModal.SetWidth(a.width)
		}
		return a, nil

	case tea.KeyMsg:
		// Global shortcuts (only when not in a modal/form)
		if a.screen == screenList || a.screen == screenThreads {
			switch msg.String() {
			case "ctrl+c":
				return a, tea.Quit
			case "?":
				a.showHelp = !a.showHelp
				return a, nil
			case "esc":
				if a.showHelp {
					a.showHelp = false
					return a, nil
				}
			case "q":
				if a.screen == screenList {
					return a, tea.Quit
				}
			}
		}
	}

	switch a.screen {
	case screenList:
		return a.updateList(msg)
	case screenThreads:
		return a.updateThreads(msg)
	case screenConfirmDelete:
		return a.updateConfirmDelete(msg)
	case screenConfirmDispatch:
		return a.updateConfirmDispatch(msg)
	case screenNoteModal:
		return a.updateNoteModal(msg)
	case screenReplyModal:
		return a.updateReplyModal(msg)
	case screenNewThreadModal:
		return a.updateNewThreadModal(msg)
	case screenEditDraftMessage:
		return a.updateEditDraftMessage(msg)
	case screenAgentAttach:
		return a.updateAgentAttach(msg)
	}
	return a, nil
}

// waitAgentChunk returns a Cmd that blocks until the next broadcast chunk arrives.
func (a *App) waitAgentChunk() tea.Cmd {
	ch := a.attachFollow
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		data, ok := <-ch
		if !ok {
			return agentDoneMsg{}
		}
		return agentChunkMsg{data: data}
	}
}

// --- List screen (split-pane) ---

func (a *App) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		// Detail-panel hotkeys — act on the currently highlighted ticket
		switch km.String() {
		case "t":
			id := a.currentTicketID()
			if id != "" {
				tv, err := views.NewThreadsView(a.store, id)
				if err != nil {
					a.setErr(err)
					return a, nil
				}
				tv.SetSize(a.width, a.height)
				a.threadsView = tv
				a.screen = screenThreads
			}
			return a, nil
		case "n":
			if a.currentTicketID() != "" {
				a.noteModal = views.NewNoteModal()
				a.screen = screenNoteModal
			}
			return a, nil
		case "[":
			if a.ticketDetail != nil {
				a.ticketDetail.ScrollUp(3)
			}
			return a, nil
		case "]":
			if a.ticketDetail != nil {
				a.ticketDetail.ScrollDown(3)
			}
			return a, nil
		case "R":
			if a.ticketDetail != nil && a.ticketDetail.Ticket() != nil && a.ticketDetail.Ticket().Status == model.StatusReady {
				id := a.currentTicketID()
				if err := a.store.TransitionTicket(id, "draft", "human"); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = fmt.Sprintf("%s → draft", id)
					a.statusErr = false
					a.ticketsView.Refresh()
					a.loadCurrentDetail()
				}
			}
			return a, nil
		case "r":
			if a.ticketDetail != nil && a.ticketDetail.Ticket() != nil && a.ticketDetail.Ticket().Status == "draft" {
				id := a.currentTicketID()
				if err := workflow.Promote(a.store, id, io.Discard, io.Discard); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = fmt.Sprintf("%s → ready", id)
					a.statusErr = false
					a.ticketsView.Refresh()
					a.loadCurrentDetail()
				}
			}
			return a, nil
		case "a":
			if a.ticketDetail != nil && a.ticketDetail.Ticket() != nil && a.ticketDetail.Ticket().Status == "in_review" {
				id := a.currentTicketID()
				t, err := a.store.GetTicket(id)
				if err != nil {
					a.setErr(err)
					return a, nil
				}
				hasOpen := false
				for _, task := range t.Tasks {
					threads, err := a.store.GetThreadsForTask(task.ID)
					if err == nil {
						for _, th := range threads {
							if th.Status == model.ThreadOpen || th.Status == model.ThreadNeedsAttention {
								hasOpen = true
							}
						}
					}
				}
				if hasOpen {
					a.statusMsg = "cannot approve: ticket has open threads"
					a.statusErr = true
				} else if err := a.store.TransitionTicket(id, "approved", "human"); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = fmt.Sprintf("%s → approved", id)
					a.statusErr = false
					a.ticketsView.Refresh()
					a.loadCurrentDetail()
				}
			}
			return a, nil
		case "m":
			if a.ticketDetail != nil && a.ticketDetail.Ticket() != nil {
				id := a.currentTicketID()
				if a.ticketDetail.Ticket().Status != "approved" {
					a.statusMsg = fmt.Sprintf("%s is not approved", id)
					a.statusErr = true
				} else if err := workflow.Merge(a.store, id, io.Discard, io.Discard); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = fmt.Sprintf("%s → merged", id)
					a.statusErr = false
					a.ticketsView.Refresh()
					a.loadCurrentDetail()
				}
			}
			return a, nil
		}

		// Tab-specific list actions
		switch km.String() {
		case "D":
			if t := a.ticketsView.SelectedTicket(); t != nil {
				a.pendingDeleteID = t.ID
				a.screen = screenConfirmDelete
			}
			return a, nil
		case "g":
			if t := a.ticketsView.SelectedTicket(); t != nil && t.Status == model.StatusReady {
				cmd, _, err := a.store.ConfigGet("agent.command")
				if err != nil || cmd == "" {
					a.statusMsg = `agent.command not configured — run: ticket config set agent.command "..."`
					a.statusErr = true
					return a, nil
				}
				sess, err := a.store.GetAgentSessionByTicket(t.ID)
				if err == nil && sess != nil {
					a.statusMsg = fmt.Sprintf("agent already active for %s (state: %s)", t.ID, sess.State)
					a.statusErr = true
					return a, nil
				}
				a.pendingDispatchID = t.ID
				a.screen = screenConfirmDispatch
			}
			return a, nil
		case "enter":
			if t := a.ticketsView.SelectedTicket(); t != nil {
				sess, _ := a.store.GetAgentSessionByTicket(t.ID)
				if sess == nil {
					sess, _ = a.store.GetLatestAgentSessionByTicket(t.ID)
				}
				if sess != nil {
					return a, a.enterAttachView(sess)
				}
			}
			return a, nil
		}
	}

	// Delegate navigation to the active list view; reload detail on cursor change
	prevID := a.selectedTicketID()
	_, cmd := a.ticketsView.Update(msg)
	if a.selectedTicketID() != prevID {
		a.loadCurrentDetail()
	}
	return a, cmd
}

// --- Threads screen ---

func (a *App) updateThreads(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.reloadCurrentDetail()
			a.screen = screenList
			return a, nil
		case "ctrl+s":
			if a.threadsView != nil {
				id := a.threadsView.TicketID()
				if err := workflow.SubmitReview(a.store, id, "human", io.Discard, io.Discard); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = fmt.Sprintf("%s → ready (review submitted)", id)
					a.statusErr = false
					a.threadsView.Reload()
					a.reloadCurrentDetail()
					a.ticketsView.Refresh()
					a.screen = screenList
				}
			}
			return a, nil
		case "e":
			if a.threadsView != nil {
				if dm := a.threadsView.SelectedDraftMessage(); dm != nil {
					a.editDraftModal = views.NewEditDraftModal(dm.ID, dm.Text)
					a.screen = screenEditDraftMessage
				}
			}
			return a, nil
		case "D":
			if a.threadsView != nil {
				if dm := a.threadsView.SelectedDraftMessage(); dm != nil {
					if err := a.store.DeleteDraftMessage(dm.ID); err != nil {
						a.setErr(err)
					} else {
						a.statusMsg = "Draft message deleted"
						a.statusErr = false
						a.threadsView.Reload()
					}
				}
			}
			return a, nil
		case "r":
			if a.threadsView != nil {
				if th := a.threadsView.SelectedThread(); th != nil {
					a.replyModal = views.NewReplyModal(th.ID, a.width)
					a.screen = screenReplyModal
				}
			}
			return a, nil
		case "n":
			if a.threadsView != nil {
				taskID := a.threadsView.SelectedTaskID()
				if taskID == "" {
					a.setErr(fmt.Errorf("no task selected"))
					return a, nil
				}
				a.newThreadModal = views.NewNewThreadModal(taskID, a.width)
				a.screen = screenNewThreadModal
			}
			return a, nil
		}
	}
	_, cmd := a.threadsView.Update(msg)
	return a, cmd
}

// --- Confirm dispatch ---

func (a *App) updateConfirmDispatch(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "y", "Y":
			id := a.pendingDispatchID
			a.pendingDispatchID = ""
			a.screen = screenList

			cmdTemplate, _, err := a.store.ConfigGet("agent.command")
			if err != nil || cmdTemplate == "" {
				a.statusMsg = `agent.command not configured`
				a.statusErr = true
				return a, nil
			}

			t, err := a.store.GetTicket(id)
			if err != nil {
				a.setErr(err)
				return a, nil
			}

			prompt, err := agent.BuildPrompt(cmdTemplate)
			if err != nil {
				a.setErr(fmt.Errorf("agent: build prompt: %w", err))
				return a, nil
			}

			_, err = a.launcher.Launch(id, t.WorktreePath, prompt)
			if err != nil {
				a.setErr(fmt.Errorf("agent launch: %w", err))
				return a, nil
			}

			a.statusMsg = fmt.Sprintf("agent dispatched to %s", id)
			a.statusErr = false
		default:
			a.pendingDispatchID = ""
			a.screen = screenList
		}
	}
	return a, nil
}

// --- Confirm delete ---

func (a *App) updateConfirmDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "y", "Y":
			if err := a.store.DeleteTicket(a.pendingDeleteID); err != nil {
				a.setErr(err)
			} else {
				a.statusMsg = fmt.Sprintf("Deleted %s", a.pendingDeleteID)
				a.statusErr = false
				a.ticketsView.Refresh()
				a.loadCurrentDetail()
			}
			a.pendingDeleteID = ""
			a.screen = screenList
		default:
			a.pendingDeleteID = ""
			a.screen = screenList
		}
	}
	return a, nil
}

// --- Note modal ---

func (a *App) updateNoteModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = screenList
			return a, nil
		case "ctrl+s", "enter":
			if a.noteModal.Focused() != 0 || km.String() == "ctrl+s" {
				id := a.currentTicketID()
				if id != "" && a.noteModal.Text() != "" {
					if _, err := a.store.AddNote(id, a.noteModal.Author(), a.noteModal.Text()); err != nil {
						a.setErr(err)
					} else {
						a.statusMsg = "Note added"
						a.statusErr = false
						a.reloadCurrentDetail()
					}
				}
				a.screen = screenList
				return a, nil
			}
		}
	}
	_, cmd := a.noteModal.Update(msg)
	return a, cmd
}

// --- Edit draft message modal ---

func (a *App) updateEditDraftMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = screenThreads
			return a, nil
		case "ctrl+s":
			if a.editDraftModal != nil && a.editDraftModal.Text() != "" {
				if err := a.store.UpdateDraftMessage(a.editDraftModal.MsgID(), a.editDraftModal.Text()); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = "Draft message updated"
					a.statusErr = false
					a.threadsView.Reload()
				}
			}
			a.screen = screenThreads
			return a, nil
		}
	}
	if a.editDraftModal != nil {
		_, cmd := a.editDraftModal.Update(msg)
		return a, cmd
	}
	return a, nil
}

// --- Reply modal ---

func (a *App) updateReplyModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = screenThreads
			return a, nil
		case "ctrl+s":
			if a.replyModal.Text() != "" {
				ticketID := ""
				if a.threadsView != nil {
					ticketID = a.threadsView.TicketID()
				}
				if _, err := a.store.AddDraftMessage(a.replyModal.ThreadID(), ticketID, true, a.replyModal.Author(), a.replyModal.Text()); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = "Reply staged (submit with ctrl+s in threads view)"
					a.statusErr = false
					a.threadsView.Reload()
				}
			}
			a.screen = screenThreads
			return a, nil
		}
	}
	_, cmd := a.replyModal.Update(msg)
	return a, cmd
}

// --- New thread modal ---

func (a *App) updateNewThreadModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = screenThreads
			return a, nil
		case "ctrl+s":
			if a.newThreadModal.Text() != "" {
				ticketID := ""
				if a.threadsView != nil {
					ticketID = a.threadsView.TicketID()
				}
				dt, err := a.store.CreateDraftThread(ticketID, a.newThreadModal.TaskID())
				if err != nil {
					a.setErr(err)
					a.screen = screenThreads
					return a, nil
				}
				if _, err := a.store.AddDraftMessage(dt.ID, ticketID, false, a.newThreadModal.Author(), a.newThreadModal.Text()); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = "Thread staged (submit with ctrl+s in threads view)"
					a.statusErr = false
					a.threadsView.Reload()
				}
			}
			a.screen = screenThreads
			return a, nil
		}
	}
	_, cmd := a.newThreadModal.Update(msg)
	return a, cmd
}

// --- View ---

func (a *App) View() string {
	var sb strings.Builder

	tabBar := a.renderTabBar()
	sb.WriteString(tabBar + "\n")
	sb.WriteString(strings.Repeat("─", a.width) + "\n")

	bodyH := a.bodyHeight()

	if a.showHelp {
		sb.WriteString(a.renderHelp())
		return sb.String()
	}

	switch a.screen {
	case screenList:
		a.ticketsView.SetSize(a.leftW, bodyH)

		leftContent := a.ticketsView.View()

		leftPane := lipgloss.NewStyle().
			Width(a.leftW).
			Height(bodyH).
			Render(leftContent)

		if a.ticketDetail != nil {
			a.ticketDetail.SetSize(a.rightW, bodyH)
		}

		var rightContent string
		if a.ticketDetail != nil {
			rightContent = a.ticketDetail.View()
		} else {
			rightContent = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Render("No ticket selected.")
		}

		rightPane := lipgloss.NewStyle().
			Width(a.rightW).
			Height(bodyH).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			Render(rightContent)

		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane))

	case screenThreads:
		if a.threadsView != nil {
			sb.WriteString(a.threadsView.View())
		}
	case screenNoteModal:
		if a.noteModal != nil {
			sb.WriteString(a.noteModal.View())
		}
	case screenReplyModal:
		if a.replyModal != nil {
			sb.WriteString(a.replyModal.View())
		}
	case screenNewThreadModal:
		if a.newThreadModal != nil {
			sb.WriteString(a.newThreadModal.View())
		}
	case screenEditDraftMessage:
		if a.editDraftModal != nil {
			sb.WriteString(a.editDraftModal.View())
		}
	case screenAgentAttach:
		title := fmt.Sprintf("Agent output: %s", a.attachTicketID)
		if a.attachEnded {
			title += "  [session ended]"
		}
		title += "  —  ctrl+] to detach"
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title) + "\n")
		sb.WriteString(strings.Repeat("─", a.width) + "\n")
		avail := a.bodyHeight() - 2
		if avail < 1 {
			avail = 1
		}
		lines := a.attachLines
		// Render exactly avail lines so view height is always constant.
		for i := 0; i < avail; i++ {
			var l string
			if i < len(lines) {
				l = lines[i]
				if len(l) > a.width {
					l = l[:a.width]
				}
			}
			sb.WriteString(l + "\n")
		}

	case screenConfirmDelete:
		prompt := fmt.Sprintf("Delete ticket %s? (y/N) ", a.pendingDeleteID)
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Render(prompt))
	case screenConfirmDispatch:
		prompt := fmt.Sprintf("Dispatch agent to %s? (y/N) ", a.pendingDispatchID)
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4")).Render(prompt))
	}

	// Status bar — always rendered to keep View() height constant.
	// Flatten newlines so a multi-line git error never overflows the reserved line.
	statusText := strings.ReplaceAll(strings.TrimSpace(a.statusMsg), "\n", " · ")
	var statusLine string
	if a.statusErr {
		statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗ " + statusText)
	} else if statusText != "" {
		statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓ " + statusText)
	}
	sb.WriteString("\n" + statusLine)

	return sb.String()
}

func (a *App) renderTabBar() string {
	label := lipgloss.NewStyle().Bold(true).Underline(true).Padding(0, 1).Render("Tickets")
	return label + "   " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("? help · q quit")
}

func (a *App) renderHelp() string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Keybindings") + "\n\n")

	sections := []struct {
		title string
		lines []string
	}{
		{"Global", []string{
			"?                 toggle help",
			"q / ctrl+c        quit",
		}},
		{"List Panel (left)", []string{
			"↑↓ / j/k          navigate",
			"D                 delete ticket",
		}},
		{"Detail Panel (right)", []string{
			"t                 threads",
			"n                 add note",
			"r                 mark ready (draft tickets)",
			"R                 back to draft (ready tickets)",
			"g                 dispatch agent (ready tickets)",
			"enter             attach to agent output (if active)",
			"ctrl+]            detach from agent output",
			"a                 approve (in_review tickets)",
			"m                 merge (approved tickets)",
			"[ / ]             scroll up / down",
		}},
		{"Threads", []string{
			"↑↓                navigate",
			"enter             expand/collapse",
			"r                 reply",
			"x                 resolve",
			"n                 new thread",
			"esc               back",
		}},
		{"Forms / Modals", []string{
			"tab               next field",
			"shift+tab         prev field",
			"ctrl+s            save/confirm",
			"esc               cancel",
		}},
	}

	for _, s := range sections {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4")).Render(s.title) + "\n")
		for _, l := range s.lines {
			sb.WriteString("  " + l + "\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("? / esc to close"))
	return sb.String()
}

// enterAttachView subscribes to the session's broadcast channel, loads the log
// as initial content, and transitions to the agent-attach overlay screen.
func (a *App) enterAttachView(sess *model.AgentSession) tea.Cmd {
	follow, unsub := a.launcher.Subscribe(sess.ID)

	term := vt10x.New(vt10x.WithSize(attachTermCols, attachTermRows))
	if data, err := os.ReadFile(sess.LogPath); err == nil && len(data) > 0 {
		term.Write(data)
	}

	a.attachFollow = follow
	a.attachUnsub = unsub
	a.attachTerm = term
	avail := a.bodyHeight() - 2
	if avail < 1 {
		avail = 1
	}
	a.attachLines = renderTermViewport(term, avail)
	a.attachTicketID = sess.TicketID
	a.attachSessionID = sess.ID
	a.attachEnded = false
	a.attachRenderSeq = 0
	a.screen = screenAgentAttach
	// waitAgentChunk is the only driver; each chunk schedules its own render tick.
	return a.waitAgentChunk()
}

// updateAgentAttach handles messages while the attach overlay is visible.
func (a *App) updateAgentAttach(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+]" {
			if a.attachUnsub != nil {
				a.attachUnsub()
				a.attachUnsub = nil
			}
			a.screen = screenList
			return a, nil
		}
		// Forward all other keys to the agent PTY.
		if ptym := a.launcher.PTYMaster(a.attachSessionID); ptym != nil {
			ptym.Write(keyMsgBytes(msg))
		}
	case agentChunkMsg:
		if a.attachTerm != nil {
			a.attachTerm.Write(msg.data)
		}
		// Schedule a snapshot 30 ms after this chunk. If more chunks arrive
		// before the timer fires, this seq is replaced and the old tick is
		// ignored — ensuring we always snapshot after the last chunk in a burst.
		a.attachRenderSeq++
		seq := a.attachRenderSeq
		renderCmd := tea.Tick(30*time.Millisecond, func(time.Time) tea.Msg {
			return agentRenderMsg{seq: seq}
		})
		return a, tea.Batch(a.waitAgentChunk(), renderCmd)
	case agentRenderMsg:
		if msg.seq != a.attachRenderSeq {
			return a, nil // stale tick from an earlier chunk burst — ignore
		}
		if a.attachTerm != nil {
			avail := a.bodyHeight() - 2
			if avail < 1 {
				avail = 1
			}
			a.attachLines = renderTermViewport(a.attachTerm, avail)
		}
	case agentDoneMsg:
		a.attachEnded = true
		if a.attachTerm != nil {
			avail := a.bodyHeight() - 2
			if avail < 1 {
				avail = 1
			}
			a.attachLines = renderTermViewport(a.attachTerm, avail)
		}
	}
	return a, nil
}

// keyMsgBytes converts a Bubble Tea key event into the byte sequence a PTY expects.
func keyMsgBytes(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyRunes:
		b := []byte(string(msg.Runes))
		if msg.Alt {
			return append([]byte{'\x1b'}, b...)
		}
		return b
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyBackspace:
		return []byte{'\x7f'}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyEscape:
		return []byte{'\x1b'}
	case tea.KeyUp:
		return []byte{'\x1b', '[', 'A'}
	case tea.KeyDown:
		return []byte{'\x1b', '[', 'B'}
	case tea.KeyRight:
		return []byte{'\x1b', '[', 'C'}
	case tea.KeyLeft:
		return []byte{'\x1b', '[', 'D'}
	case tea.KeyHome:
		return []byte{'\x1b', '[', 'H'}
	case tea.KeyEnd:
		return []byte{'\x1b', '[', 'F'}
	case tea.KeyDelete:
		return []byte{'\x1b', '[', '3', '~'}
	case tea.KeyPgUp:
		return []byte{'\x1b', '[', '5', '~'}
	case tea.KeyPgDown:
		return []byte{'\x1b', '[', '6', '~'}
	case tea.KeyCtrlA:
		return []byte{1}
	case tea.KeyCtrlB:
		return []byte{2}
	case tea.KeyCtrlC:
		return []byte{3}
	case tea.KeyCtrlD:
		return []byte{4}
	case tea.KeyCtrlE:
		return []byte{5}
	case tea.KeyCtrlF:
		return []byte{6}
	case tea.KeyCtrlG:
		return []byte{7}
	case tea.KeyCtrlH:
		return []byte{8}
	case tea.KeyCtrlJ:
		return []byte{'\r'}
	case tea.KeyCtrlK:
		return []byte{11}
	case tea.KeyCtrlL:
		return []byte{12}
	case tea.KeyCtrlN:
		return []byte{14}
	case tea.KeyCtrlO:
		return []byte{15}
	case tea.KeyCtrlP:
		return []byte{16}
	case tea.KeyCtrlQ:
		return []byte{17}
	case tea.KeyCtrlR:
		return []byte{18}
	case tea.KeyCtrlS:
		return []byte{19}
	case tea.KeyCtrlT:
		return []byte{20}
	case tea.KeyCtrlU:
		return []byte{21}
	case tea.KeyCtrlV:
		return []byte{22}
	case tea.KeyCtrlW:
		return []byte{23}
	case tea.KeyCtrlX:
		return []byte{24}
	case tea.KeyCtrlY:
		return []byte{25}
	case tea.KeyCtrlZ:
		return []byte{26}
	}
	return nil
}

// terminateSilentReviewedSessions terminates any agent session whose ticket is
// in_review and has been silent long enough to reach the "waiting" state.
// Called on every DB tick so the session is cleaned up promptly after the agent
// finishes its work and submits for review.
func (a *App) terminateSilentReviewedSessions() {
	sessions, err := a.store.ListActiveAgentSessions()
	if err != nil {
		return
	}
	for _, sess := range sessions {
		if sess.State != model.AgentWaiting {
			continue
		}
		ticket, err := a.store.GetTicket(sess.TicketID)
		if err != nil || ticket == nil {
			continue
		}
		if ticket.Status == model.StatusInReview {
			a.launcher.Terminate(sess.TicketID)
		}
	}
}

// --- helpers ---

func (a *App) setErr(err error) {
	a.statusMsg = err.Error()
	a.statusErr = true
}

func (a *App) reloadCurrentDetail() {
	if a.ticketDetail != nil {
		_ = a.ticketDetail.Reload()
	}
}
