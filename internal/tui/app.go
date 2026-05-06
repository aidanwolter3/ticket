package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/views"
	"github.com/aidanwolter/ticket/internal/workflow"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
)

type dbTickMsg struct{}

func tickDB() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return dbTickMsg{} })
}

type App struct {
	store           *store.Store
	tab             appTab
	screen          appScreen
	width           int
	height          int
	leftW           int
	rightW          int
	statusMsg       string
	statusErr       bool
	pendingDeleteID string
	showHelp        bool

	// list views (left pane)
	ticketsView *views.TicketsView

	// detail view (right pane)
	ticketDetail *views.TicketDetailView

	// overlay screens
	threadsView    *views.ThreadsView
	noteModal      *views.NoteModal
	replyModal     *views.ReplyModal
	newThreadModal *views.NewThreadModal
}

func New(s *store.Store) *App {
	a := &App{
		store:       s,
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
	case screenNoteModal:
		return a.updateNoteModal(msg)
	case screenReplyModal:
		return a.updateReplyModal(msg)
	case screenNewThreadModal:
		return a.updateNewThreadModal(msg)
	}
	return a, nil
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
		case "r":
			if a.threadsView != nil {
				if th := a.threadsView.SelectedThread(); th != nil {
					a.replyModal = views.NewReplyModal(th.ID)
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
				a.newThreadModal = views.NewNewThreadModal(taskID)
				a.screen = screenNewThreadModal
			}
			return a, nil
		}
	}
	_, cmd := a.threadsView.Update(msg)
	return a, cmd
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

// --- Reply modal ---

func (a *App) updateReplyModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = screenThreads
			return a, nil
		case "ctrl+s":
			if a.replyModal.Text() != "" {
				if _, err := a.store.AddMessage(a.replyModal.ThreadID(), a.replyModal.Author(), a.replyModal.Text()); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = "Reply added"
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
				thread, err := a.store.CreateThread(a.newThreadModal.TaskID())
				if err != nil {
					a.setErr(err)
					a.screen = screenThreads
					return a, nil
				}
				if _, err := a.store.AddMessage(thread.ID, a.newThreadModal.Author(), a.newThreadModal.Text()); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = "Thread created"
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
	case screenConfirmDelete:
		prompt := fmt.Sprintf("Delete ticket %s? (y/N) ", a.pendingDeleteID)
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Render(prompt))
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
