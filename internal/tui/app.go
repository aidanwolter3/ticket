package tui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aidanwolter/ticket/internal/agent"
	"github.com/aidanwolter/ticket/internal/bubbleterm/emulator"
	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/views"
	"github.com/aidanwolter/ticket/internal/workflow"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// agentChunkMsg carries rendered lines from the emulator broadcast channel.
type agentChunkMsg struct{ lines []string }

// agentDoneMsg is sent when the attach channel closes (session ended).
type agentDoneMsg struct{}

type appTab int

const (
	tabTickets appTab = iota
)

type appScreen int

const (
	screenList appScreen = iota // split-pane: list left, detail right
	screenThreads
	screenReviewPanel
	screenNoteModal
	screenReplyModal
	screenNewThreadModal
	screenConfirmDelete
	screenConfirmRedraft
	screenEditDraftMessage
	screenConfirmDispatch
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
	pendingDeleteID   string
	pendingRedraftID  string
	pendingDispatchID string

	// list views (left pane)
	ticketsView *views.TicketsView

	// detail view (right pane)
	ticketDetail *views.TicketDetailView

	// overlay screens
	threadsView        *views.ThreadsView
	threadsReturnScreen appScreen // screen to return to when esc-ing from threadsView
	reviewPanelView    *views.ReviewPanelView
	noteModal          *views.NoteModal
	replyModal         *views.ReplyModal
	newThreadModal     *views.NewThreadModal
	newThreadReturn    appScreen // screen to return to after newThreadModal
	editDraftModal     *views.EditDraftModal

	// agent attach state
	attachFollow    <-chan []string
	attachUnsub     func()
	attachTicketID  string
	attachSessionID string
	rightPaneMode   string // "detail" or "agent"
	agentTermView   *views.AgentTermView
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

// Update dispatches incoming messages to global handlers or the active screen handler.
//
// Message-type ownership:
//
//	dbTickMsg         – global: refreshes all live views and reschedules the DB poll tick
//	tea.WindowSizeMsg – global: resizes every live view component
//	agentChunkMsg     – screenList: updates the agent-terminal right pane with new output lines
//	agentDoneMsg      – screenList: marks the agent-terminal pane as idle when the session ends
//	tea.KeyMsg        – global subset (ctrl+c, ?, esc, q) handled here;
//	                    remaining keys delegated to the active screen handler
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
		if a.reviewPanelView != nil {
			a.reviewPanelView.SetSize(a.width, a.height)
		}
		if a.replyModal != nil {
			a.replyModal.SetWidth(a.width)
		}
		if a.newThreadModal != nil {
			a.newThreadModal.SetWidth(a.width)
		}
		if a.agentTermView != nil {
			a.agentTermView.SetSize(a.rightW, a.bodyHeight())
			a.launcher.ResizeSession(a.attachSessionID, a.rightW, a.bodyHeight()) //nolint:errcheck
		}
		return a, nil

	case tea.KeyMsg:
		// Global shortcuts (only when not in a modal/form, and not when the agent
		// pane is focused — all keys except ctrl+] are forwarded to the agent PTY).
		if (a.screen == screenList || a.screen == screenThreads || a.screen == screenReviewPanel) && a.rightPaneMode != "agent" {
			switch msg.String() {
			case "ctrl+c":
				return a, tea.Quit
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
	case screenReviewPanel:
		return a.updateReviewPanel(msg)
	case screenConfirmDelete:
		return a.updateConfirmDelete(msg)
	case screenConfirmRedraft:
		return a.updateConfirmRedraft(msg)
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
	}
	return a, nil
}

// waitAgentChunk returns a Cmd that blocks until the next broadcast frame arrives.
func (a *App) waitAgentChunk() tea.Cmd {
	ch := a.attachFollow
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		lines, ok := <-ch
		if !ok {
			return agentDoneMsg{}
		}
		return agentChunkMsg{lines: lines}
	}
}

// --- List screen (split-pane) ---

func (a *App) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case agentChunkMsg:
		if a.agentTermView != nil {
			a.agentTermView.SetLines(msg.lines)
			a.agentTermView.SetState(true, false)
		}
		return a, a.waitAgentChunk()

	case agentDoneMsg:
		if a.agentTermView != nil {
			a.agentTermView.SetState(false, false)
		}
		return a, nil
	}

	if km, ok := msg.(tea.KeyMsg); ok {
		// Forward keys to the agent PTY when agent pane is active.
		if a.rightPaneMode == "agent" {
			switch km.String() {
			case "ctrl+]":
				// handled by the list handler below to detach — don't forward
			case "shift+tab":
				// handled by the list handler below to cycle agents — don't forward
			default:
				a.launcher.WriteToAgent(a.attachSessionID, keyMsgBytes(km)) //nolint:errcheck
				return a, nil
			}
		}
		// Detail-panel hotkeys — act on the currently highlighted ticket
		switch km.String() {
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
		case "X":
			if a.ticketDetail != nil {
				if t := a.ticketDetail.Ticket(); t != nil {
					st := t.Status
					if st == model.StatusReady || st == model.StatusInProgress || st == model.StatusInReview {
						a.pendingRedraftID = t.ID
						a.screen = screenConfirmRedraft
					}
				}
			}
			return a, nil
		case "R":
			if a.ticketDetail != nil && a.ticketDetail.Ticket() != nil {
				t := a.ticketDetail.Ticket()
				if t.Status == model.StatusInReview {
					id := a.currentTicketID()
					rpv, err := views.NewReviewPanelView(a.store, id)
					if err != nil {
						a.setErr(err)
					} else {
						rpv.SetSize(a.width, a.height)
						a.reviewPanelView = rpv
						a.screen = screenReviewPanel
					}
				}
			}
			return a, nil
		case "r":
			if a.ticketDetail != nil && a.ticketDetail.Ticket() != nil && a.ticketDetail.Ticket().Status == "draft" {
				id := a.currentTicketID()
				if err := workflow.Promote(a.store, id, a.launcher, io.Discard, io.Discard); err != nil {
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
				} else if err := a.store.TransitionTicket(id, model.StatusApproved); err != nil {
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
		case "C":
			if a.ticketDetail != nil && a.ticketDetail.Ticket() != nil && (a.ticketDetail.Ticket().Status == model.StatusApproved || a.ticketDetail.Ticket().Status == model.StatusInReview) {
				t := a.ticketDetail.Ticket()
				last, err := a.store.LastTaskForTicket(t.ID)
				if err != nil {
					a.setErr(err)
					return a, nil
				}
				position := 1
				if last != nil {
					position = last.Position + 1
				}
				task := &model.Task{
					TicketID: t.ID,
					Title:    "Sync this worktree so that it has the latest commits from 'main' and fix any merge conflicts",
					Position: position,
				}
				if err := a.store.CreateTask(task); err != nil {
					a.setErr(err)
					return a, nil
				}
				if err := a.store.TransitionTicket(t.ID, model.StatusReady); err != nil {
					a.setErr(err)
					return a, nil
				}
				a.statusMsg = fmt.Sprintf("%s → ready (resolve-conflicts task added)", t.ID)
				a.statusErr = false
				a.ticketsView.Refresh()
				a.loadCurrentDetail()
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
		case "shift+tab":
			if id := a.nextWaitingTicketID(); id != "" {
				a.ticketsView.SelectTicketByID(id)
				a.loadCurrentDetail()
				sess, _ := a.store.GetAgentSessionByTicket(id)
				if sess != nil {
					a.rightPaneMode = "agent"
					atv := views.NewAgentTermView(id)
					atv.SetSize(a.rightW, a.bodyHeight())
					a.agentTermView = atv
					return a, a.enterAttachView(sess)
				}
			}
			return a, nil
		case "ctrl+]":
			if a.rightPaneMode == "agent" {
				if a.attachUnsub != nil {
					a.attachUnsub()
					a.attachUnsub = nil
				}
				a.rightPaneMode = "detail"
				a.agentTermView = nil
			}
			return a, nil
		case "enter":
			if t := a.ticketsView.SelectedTicket(); t != nil {
				sess, _ := a.store.GetAgentSessionByTicket(t.ID)
				if sess == nil {
					sess, _ = a.store.GetLatestAgentSessionByTicket(t.ID)
				}
				if sess != nil {
					a.rightPaneMode = "agent"
					atv := views.NewAgentTermView(sess.TicketID)
					atv.SetSize(a.rightW, a.bodyHeight())
					a.agentTermView = atv
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
			returnTo := a.threadsReturnScreen
			a.threadsReturnScreen = 0
			if returnTo == screenReviewPanel {
				a.screen = screenReviewPanel
			} else {
				a.reloadCurrentDetail()
				a.screen = screenList
			}
			return a, nil
		case "ctrl+s":
			if a.threadsView != nil {
				id := a.threadsView.TicketID()
				if err := workflow.SubmitReview(a.store, id, "human", nil, io.Discard, io.Discard); err != nil {
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
				a.newThreadModal = views.NewNewThreadModal(taskID, "", "", a.width)
				a.newThreadReturn = screenThreads
				a.screen = screenNewThreadModal
			}
			return a, nil
		}
	}
	_, cmd := a.threadsView.Update(msg)
	return a, cmd
}

// --- Review panel screen ---

func (a *App) updateReviewPanel(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.reloadCurrentDetail()
			a.screen = screenList
			return a, nil
		case "c":
			if a.reviewPanelView != nil {
				taskID := a.reviewPanelView.SelectedTaskID()
				if taskID == "" {
					a.setErr(fmt.Errorf("no task selected"))
					return a, nil
				}
				filePath, hunkHeader := a.reviewPanelView.HunkContext()
				a.newThreadModal = views.NewNewThreadModal(taskID, filePath, hunkHeader, a.width)
				a.newThreadReturn = screenReviewPanel
				a.screen = screenNewThreadModal
			}
			return a, nil
		case "v":
			if a.reviewPanelView != nil {
				id := a.reviewPanelView.TicketID()
				tv, err := views.NewThreadsView(a.store, id)
				if err != nil {
					a.setErr(err)
				} else {
					tv.SetSize(a.width, a.height)
					a.threadsView = tv
					a.threadsReturnScreen = screenReviewPanel
					a.screen = screenThreads
				}
			}
			return a, nil
		case "a":
			if a.reviewPanelView != nil {
				id := a.reviewPanelView.TicketID()
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
				} else if err := a.store.TransitionTicket(id, model.StatusApproved); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = fmt.Sprintf("%s → approved", id)
					a.statusErr = false
					a.ticketsView.Refresh()
					a.loadCurrentDetail()
					a.screen = screenList
				}
			}
			return a, nil
		case "S":
			if a.reviewPanelView != nil {
				id := a.reviewPanelView.TicketID()
				if err := workflow.SubmitReview(a.store, id, "human", a.launcher, io.Discard, io.Discard); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = fmt.Sprintf("%s → ready (review submitted)", id)
					a.statusErr = false
					a.reloadCurrentDetail()
					a.ticketsView.Refresh()
					a.screen = screenList
				}
			}
			return a, nil
		}
	}
	if a.reviewPanelView != nil {
		_, cmd := a.reviewPanelView.Update(msg)
		return a, cmd
	}
	return a, nil
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

			if t.WorktreePath == "" && t.RepoPath != "" {
				featureBranch := t.FeatureBranch
				if featureBranch == "" {
					featureBranch = "feat/" + strings.ToLower(id)
				}
				worktreeAbs := filepath.Join(t.RepoPath, ".worktrees", id)
				checkBranch := exec.Command("git", "-C", t.RepoPath, "rev-parse", "--verify", featureBranch)
				checkBranch.Stdout = io.Discard
				checkBranch.Stderr = io.Discard
				branchExists := checkBranch.Run() == nil
				var wtCmd *exec.Cmd
				if branchExists {
					wtCmd = exec.Command("git", "-C", t.RepoPath, "worktree", "add", worktreeAbs, featureBranch)
				} else {
					wtCmd = exec.Command("git", "-C", t.RepoPath, "worktree", "add", "-b", featureBranch, worktreeAbs)
				}
				var wtStderr bytes.Buffer
				wtCmd.Stderr = &wtStderr
				if wtErr := wtCmd.Run(); wtErr != nil {
					msg := strings.TrimSpace(wtStderr.String())
					if msg == "" {
						msg = wtErr.Error()
					}
					a.setErr(fmt.Errorf("create worktree: %s", msg))
					return a, nil
				}
				if saveErr := a.store.SetWorktreePath(id, worktreeAbs, t.RepoPath, featureBranch); saveErr != nil {
					exec.Command("git", "-C", t.RepoPath, "worktree", "remove", "--force", worktreeAbs).Run() //nolint:errcheck
					a.setErr(fmt.Errorf("save worktree_path: %w", saveErr))
					return a, nil
				}
				t.WorktreePath = worktreeAbs
			}

			_, err = a.launcher.Launch(id, t.WorktreePath, prompt)
			if err != nil {
				a.setErr(fmt.Errorf("agent launch: %w", err))
				return a, nil
			}
			if transErr := a.store.TransitionTicket(id, model.StatusInProgress); transErr != nil {
				a.setErr(fmt.Errorf("transition in_progress: %w", transErr))
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

// --- Confirm redraft ---

func (a *App) updateConfirmRedraft(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "y", "Y":
			id := a.pendingRedraftID
			a.pendingRedraftID = ""
			a.screen = screenList
			if err := workflow.Redraft(a.store, id, io.Discard, io.Discard); err != nil {
				a.setErr(err)
			} else {
				a.statusMsg = fmt.Sprintf("%s → draft", id)
				a.statusErr = false
				a.ticketsView.Refresh()
				a.loadCurrentDetail()
			}
		default:
			a.pendingRedraftID = ""
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
	returnTo := a.newThreadReturn
	if returnTo == 0 {
		returnTo = screenThreads
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = returnTo
			return a, nil
		case "ctrl+s", "S":
			if a.newThreadModal.Text() != "" {
				ticketID := ""
				if a.threadsView != nil {
					ticketID = a.threadsView.TicketID()
				}
				if ticketID == "" && a.reviewPanelView != nil {
					ticketID = a.reviewPanelView.TicketID()
				}
				filePath := a.newThreadModal.FilePath()
				hunkHeader := a.newThreadModal.HunkHeader()
				dt, err := a.store.CreateDraftThread(ticketID, a.newThreadModal.TaskID(), filePath, hunkHeader)
				if err != nil {
					a.setErr(err)
					a.screen = returnTo
					return a, nil
				}
				if _, err := a.store.AddDraftMessage(dt.ID, ticketID, false, a.newThreadModal.Author(), a.newThreadModal.Text()); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = "Thread staged (submit with [S] in review panel)"
					a.statusErr = false
					if a.threadsView != nil {
						a.threadsView.Reload()
					}
					if a.reviewPanelView != nil {
						a.reviewPanelView.Reload()
					}
				}
			}
			a.screen = returnTo
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

	switch a.screen {
	case screenList:
		a.ticketsView.SetSize(a.leftW, bodyH)
		a.ticketsView.SetAgentFocused(a.rightPaneMode == "agent")

		leftContent := a.ticketsView.View()

		leftPane := lipgloss.NewStyle().
			Width(a.leftW).
			Height(bodyH).
			Render(leftContent)

		if a.ticketDetail != nil {
			a.ticketDetail.SetSize(a.rightW, bodyH)
		}

		var rightContent string
		if a.rightPaneMode == "agent" && a.agentTermView != nil {
			a.agentTermView.SetSize(a.rightW, bodyH)
			rightContent = a.agentTermView.View()
		} else if a.ticketDetail != nil {
			rightContent = a.ticketDetail.View()
		} else {
			rightContent = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Render("No ticket selected.")
		}

		rightPaneStyle := lipgloss.NewStyle().
			Width(a.rightW).
			Height(bodyH).
			Border(lipgloss.NormalBorder(), false, false, false, true)
		if a.rightPaneMode == "agent" {
			rightPaneStyle = rightPaneStyle.BorderForeground(lipgloss.Color("2"))
		}
		rightPane := rightPaneStyle.Render(rightContent)

		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane))

	case screenThreads:
		if a.threadsView != nil {
			sb.WriteString(a.threadsView.View())
		}
	case screenReviewPanel:
		if a.reviewPanelView != nil {
			sb.WriteString(a.reviewPanelView.View())
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
	case screenConfirmDelete:
		prompt := fmt.Sprintf("Delete ticket %s? (y/N) ", a.pendingDeleteID)
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Render(prompt))
	case screenConfirmRedraft:
		prompt := fmt.Sprintf("Revert %s to draft? This will destroy the worktree and kill any active agent. (y/N) ", a.pendingRedraftID)
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3")).Render(prompt))
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
	label := lipgloss.NewStyle().Bold(true).Underline(true).UnderlineSpaces(false).Padding(0, 1).Render("Tickets")
	hintText := "q quit"
	if a.nextWaitingTicketID() != "" {
		hintText = "shift+tab cycle agents · q quit"
	}
	hints := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(hintText)
	return label + "   " + hints
}

// enterAttachView subscribes to the session's broadcast channel, renders the log
// for initial content via the bubbleterm emulator, and transitions to the
// agent-attach overlay screen.
func (a *App) enterAttachView(sess *model.AgentSession) tea.Cmd {
	follow, unsub := a.launcher.Subscribe(sess.ID)

	// Render log history via a one-shot emulator for correct VT interpretation.
	var initialLines []string
	if em, err := emulator.New(agent.PTYCols, agent.PTYRows); err == nil {
		if logData, err := os.ReadFile(sess.LogPath); err == nil && len(logData) > 0 {
			em.FeedBytes(logData)
			frame := em.GetScreen()
			initialLines = frame.Rows
		}
		em.Close()
	}

	a.attachFollow = follow
	a.attachUnsub = unsub
	a.attachTicketID = sess.TicketID
	a.attachSessionID = sess.ID
	a.launcher.ResizeSession(sess.ID, a.rightW, a.bodyHeight()) //nolint:errcheck
	if a.agentTermView != nil && len(initialLines) > 0 {
		a.agentTermView.SetLines(initialLines)
	}
	return a.waitAgentChunk()
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

// nextWaitingTicketID returns the ticket ID of the next agent in the "waiting"
// state after the currently focused ticket, cycling through the visible list.
// Returns empty string if no agents are waiting.
func (a *App) nextWaitingTicketID() string {
	sessions, err := a.store.ListActiveAgentSessions()
	if err != nil {
		return ""
	}
	waiting := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		if sess.State == model.AgentWaiting {
			waiting[sess.TicketID] = true
		}
	}
	if len(waiting) == 0 {
		return ""
	}

	tickets := a.ticketsView.VisibleTickets()
	if len(tickets) == 0 {
		return ""
	}

	// Find index of the currently focused ticket.
	currentID := a.selectedTicketID()
	start := 0
	for i, t := range tickets {
		if t.ID == currentID {
			start = i
			break
		}
	}

	// Scan from start+1, wrapping around, to find the next waiting ticket.
	n := len(tickets)
	for offset := 1; offset <= n; offset++ {
		t := tickets[(start+offset)%n]
		if waiting[t.ID] {
			return t.ID
		}
	}
	return ""
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
