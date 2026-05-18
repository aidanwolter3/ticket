package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aidanwolter/ticket/internal/agent"
	"github.com/taigrr/bubbleterm/emulator"
	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/tui/views"
	"github.com/aidanwolter/ticket/internal/workflow/human"
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
	screenReplyModal
	screenNewThreadModal
	screenConfirmDelete
	screenConfirmRedraft
	screenEditDraftMessage
	screenConfirmDispatch
	screenPreparing
	screenTearingDown
	screenConfirmMarkMerged
)

type dbTickMsg struct{}

func tickDB() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return dbTickMsg{} })
}

type App struct {
	wf               *human.Workflow
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
	replyModal         *views.ReplyModal
	replyModalReturn   appScreen // screen to return to after replyModal
	newThreadModal     *views.NewThreadModal
	newThreadReturn    appScreen // screen to return to after newThreadModal
	editDraftModal     *views.EditDraftModal
	editDraftReturn    appScreen // screen to return to after editDraftModal

	// agent attach state
	attachFollow    <-chan []string
	attachUnsub     func()
	attachTicketID  string
	attachSessionID string
	rightPaneMode   string // "detail" or "agent"
	agentTermView   *views.AgentTermView

	// workspace operation state (preparing / tearing_down screens)
	workspaceLines    []string
	workspaceTicketID string
	workspaceRunning  bool
	workspacePurpose  string   // "dispatch", "redraft", or "mark-merged"
	workspaceScanNext tea.Cmd  // set once by spawn helpers; must not be overwritten by handler

	// custom workspace flag (read once per DB tick)
	isCustomWorkspace   bool
	pendingMarkMergedID string
}

// workspaceChunkMsg carries one line of output from the running workspace command.
type workspaceChunkMsg struct{ line string }

// workspaceDoneMsg is sent when the workspace command finishes.
type workspaceDoneMsg struct{ err error }

func New(wf *human.Workflow) *App {
	a := &App{
		wf:          wf,
		launcher:    wf.Launcher(),
		tab:         tabTickets,
		screen:      screenList,
		width:       80,
		height:      24,
		leftW:       28,
		rightW:      51,
		ticketsView: views.NewTicketsView(wf),
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
	td, err := views.NewTicketDetailView(a.wf, id)
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
		} else if a.selectedTicketID() != "" {
			a.loadCurrentDetail()
		}
		if a.threadsView != nil {
			_ = a.threadsView.Reload()
		}
		a.terminateSilentReviewedSessions()
		wsType, _ := a.wf.ConfigGetDefault("workspace.type", "worktree")
		a.isCustomWorkspace = wsType != "worktree"
		a.ticketsView.SetIsCustomWorkspace(a.isCustomWorkspace)
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

	case workspaceChunkMsg:
		a.workspaceLines = append(a.workspaceLines, msg.line)
		return a, a.workspaceScanNext

	case workspaceDoneMsg:
		a.workspaceRunning = false
		if msg.err != nil {
			a.statusMsg = msg.err.Error()
			a.statusErr = true
		} else {
			switch a.workspacePurpose {
			case "dispatch":
				a.statusMsg = a.workspaceTicketID + " dispatched"
			case "redraft":
				a.statusMsg = a.workspaceTicketID + " → draft"
			case "mark-merged":
				a.statusMsg = a.workspaceTicketID + " → merged"
			}
			a.statusErr = false
		}
		a.screen = screenList
		a.ticketsView.Refresh()
		a.loadCurrentDetail()
		return a, nil

	case tea.KeyMsg:
		// Global shortcuts (only when not in a modal/form, and not when the agent
		// pane is focused — all keys except ctrl+] are forwarded to the agent PTY).
		if (a.screen == screenList || a.screen == screenThreads || a.screen == screenReviewPanel ||
			a.screen == screenPreparing || a.screen == screenTearingDown) && a.rightPaneMode != "agent" {
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
	case screenReplyModal:
		return a.updateReplyModal(msg)
	case screenNewThreadModal:
		return a.updateNewThreadModal(msg)
	case screenEditDraftMessage:
		return a.updateEditDraftMessage(msg)
	case screenPreparing:
		return a.updateScreenPreparing(msg)
	case screenTearingDown:
		return a.updateScreenTearingDown(msg)
	case screenConfirmMarkMerged:
		return a.updateConfirmMarkMerged(msg)
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
	a.ticketsView.SetIsCustomWorkspace(a.isCustomWorkspace)

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
					if st == model.StatusReady || st == model.StatusInProgress || st == model.StatusInReview ||
						st == model.StatusPreparing || st == model.StatusTearingDown {
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
					rpv, err := views.NewReviewPanelView(a.wf, id)
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
				if err := a.wf.Ready(id, io.Discard, io.Discard); err != nil {
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
				t, err := a.wf.GetTicket(id)
				if err != nil {
					a.setErr(err)
					return a, nil
				}
				hasOpen := false
				for _, task := range t.Tasks {
					threads, err := a.wf.GetThreadsForTask(task.ID)
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
				} else if err := a.wf.TransitionTicket(id, model.StatusApproved); err != nil {
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
				} else if err := a.wf.Merge(id, io.Discard, io.Discard); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = fmt.Sprintf("%s → merged", id)
					a.statusErr = false
					a.ticketsView.Refresh()
					a.loadCurrentDetail()
				}
			}
			return a, nil
		case "M":
			if a.isCustomWorkspace && a.ticketDetail != nil && a.ticketDetail.Ticket() != nil &&
				a.ticketDetail.Ticket().Status == model.StatusApproved {
				a.pendingMarkMergedID = a.ticketDetail.Ticket().ID
				a.screen = screenConfirmMarkMerged
			}
			return a, nil
		case "C":
			if a.ticketDetail != nil && a.ticketDetail.Ticket() != nil && (a.ticketDetail.Ticket().Status == model.StatusApproved || a.ticketDetail.Ticket().Status == model.StatusInReview) {
				t := a.ticketDetail.Ticket()
				maxRound := 1
				for _, tk := range t.Tasks {
					if tk.Round > maxRound {
						maxRound = tk.Round
					}
				}
				if _, err := a.wf.AddTask(t.ID,
					"Sync this worktree so that it has the latest commits from 'main' and fix any merge conflicts",
					"", "", false, maxRound+1,
				); err != nil {
					a.setErr(err)
					return a, nil
				}
				if err := a.wf.TransitionTicket(t.ID, model.StatusReady); err != nil {
					a.setErr(err)
					return a, nil
				}
				a.statusMsg = fmt.Sprintf("%s → ready (resolve-conflicts task added)", t.ID)
				a.statusErr = false
				autoDispatch, _, _ := a.wf.ConfigGet("agent.auto_dispatch")
				cmdTemplate, _, _ := a.wf.ConfigGet("agent.command")
				if autoDispatch == "true" && cmdTemplate != "" {
					existing, _ := a.wf.GetAgentSessionByTicket(t.ID)
					if existing == nil {
						if prompt, pErr := agent.BuildPrompt(cmdTemplate); pErr == nil {
							if _, launchErr := a.launcher.Launch(t.ID, t.WorktreePath, prompt); launchErr != nil {
								a.statusMsg = fmt.Sprintf("auto-dispatch failed: %v", launchErr)
								a.statusErr = true
							} else {
								if transErr := a.wf.TransitionTicket(t.ID, model.StatusInProgress); transErr != nil {
									a.statusMsg = fmt.Sprintf("auto-dispatch: transition in_progress: %v", transErr)
									a.statusErr = true
								}
								a.wf.AddNote(t.ID, "agent:claude", "Agent auto-dispatched") //nolint:errcheck
							}
						}
					}
				}
				a.ticketsView.Refresh()
				a.loadCurrentDetail()
			}
			return a, nil
		}

		// Tab-specific list actions
		switch km.String() {
		case "b":
			if t := a.ticketsView.SelectedTicket(); t != nil && t.Status == model.StatusDraft {
				newVal := !t.Backlog
				if err := a.wf.SetBacklog(t.ID, newVal); err != nil {
					a.setErr(err)
				} else {
					if newVal {
						a.statusMsg = fmt.Sprintf("%s → backlog", t.ID)
					} else {
						a.statusMsg = fmt.Sprintf("%s → active", t.ID)
					}
					a.statusErr = false
					a.ticketsView.Refresh()
					a.loadCurrentDetail()
				}
			}
			return a, nil
		case "D":
			if t := a.ticketsView.SelectedTicket(); t != nil {
				a.pendingDeleteID = t.ID
				a.screen = screenConfirmDelete
			}
			return a, nil
		case "g":
			if t := a.ticketsView.SelectedTicket(); t != nil && t.Status == model.StatusReady {
				cmd, _, err := a.wf.ConfigGet("agent.command")
				if err != nil || cmd == "" {
					a.statusMsg = `agent.command not configured — run: ticket config set agent.command "..."`
					a.statusErr = true
					return a, nil
				}
				sess, err := a.wf.GetAgentSessionByTicket(t.ID)
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
				sess, _ := a.wf.GetAgentSessionByTicket(id)
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
				if t.Status == model.StatusPreparing || t.Status == model.StatusTearingDown {
					logPath := filepath.Join(os.TempDir(), "ticket-workspace-"+t.ID+".log")
					logData, _ := os.ReadFile(logPath)
					lines := strings.Split(strings.TrimRight(string(logData), "\n"), "\n")
					if len(lines) == 1 && lines[0] == "" {
						lines = nil
					}
					a.workspaceLines = lines
					a.workspaceTicketID = t.ID
					if t.Status == model.StatusPreparing {
						a.screen = screenPreparing
					} else {
						a.screen = screenTearingDown
					}
					return a, nil
				}
				sess, _ := a.wf.GetAgentSessionByTicket(t.ID)
				if sess == nil {
					sess, _ = a.wf.GetLatestAgentSessionByTicket(t.ID)
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
				if err := a.wf.SubmitReview(id, "human", io.Discard, io.Discard); err != nil {
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
		case "e":
			if a.threadsView != nil {
				if dm := a.threadsView.SelectedDraftMessage(); dm != nil {
					a.editDraftModal = views.NewEditDraftModal(dm.ID, dm.Text)
					a.editDraftReturn = screenThreads
					a.screen = screenEditDraftMessage
				}
			}
			return a, nil
		case "D":
			if a.threadsView != nil {
				if dm := a.threadsView.SelectedDraftMessage(); dm != nil {
					if err := a.wf.DeleteDraftMessage(dm.ID); err != nil {
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
					a.replyModalReturn = screenThreads
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
		case "r":
			if a.reviewPanelView != nil {
				if th := a.reviewPanelView.SelectedThread(); th != nil {
					a.replyModal = views.NewReplyModal(th.ID, a.width)
					a.replyModalReturn = screenReviewPanel
					a.screen = screenReplyModal
				}
			}
			return a, nil
		case "x":
			if a.reviewPanelView != nil {
				if th := a.reviewPanelView.SelectedThread(); th != nil {
					ticketID := a.reviewPanelView.TicketID()
					switch th.Status {
					case model.ThreadOpen, model.ThreadNeedsAttention:
						if ds, _ := a.wf.GetDraftState(ticketID); ds != nil && ds.ActionFor(th.ID) == model.DraftActionResolve {
							if err := a.wf.ClearDraftAction(th.ID); err != nil {
								a.setErr(err)
							}
						} else {
							if err := a.wf.SetDraftAction(th.ID, ticketID, model.DraftActionResolve); err != nil {
								a.setErr(err)
							}
						}
					case model.ThreadResolved:
						if ds, _ := a.wf.GetDraftState(ticketID); ds != nil && ds.ActionFor(th.ID) == model.DraftActionReopen {
							if err := a.wf.ClearDraftAction(th.ID); err != nil {
								a.setErr(err)
							}
						} else {
							if err := a.wf.SetDraftAction(th.ID, ticketID, model.DraftActionReopen); err != nil {
								a.setErr(err)
							}
						}
					}
					a.reviewPanelView.Reload()
				}
			}
			return a, nil
		case "a":
			if a.reviewPanelView != nil {
				id := a.reviewPanelView.TicketID()
				t, err := a.wf.GetTicket(id)
				if err != nil {
					a.setErr(err)
					return a, nil
				}
				hasOpen := false
				for _, task := range t.Tasks {
					threads, err := a.wf.GetThreadsForTask(task.ID)
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
				} else if err := a.wf.TransitionTicket(id, model.StatusApproved); err != nil {
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
		case "ctrl+s":
			if a.reviewPanelView != nil {
				id := a.reviewPanelView.TicketID()
				if err := a.wf.SubmitReview(id, "human", io.Discard, io.Discard); err != nil {
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
		case "e":
			if a.reviewPanelView != nil {
				if dm := a.reviewPanelView.SelectedDraftMessage(); dm != nil {
					a.editDraftModal = views.NewEditDraftModal(dm.ID, dm.Text)
					a.editDraftReturn = screenReviewPanel
					a.screen = screenEditDraftMessage
				}
			}
			return a, nil
		case "D":
			if a.reviewPanelView != nil {
				if dm := a.reviewPanelView.SelectedDraftMessage(); dm != nil {
					if err := a.wf.DeleteDraftMessage(dm.ID); err != nil {
						a.setErr(err)
					} else {
						a.statusMsg = "Draft message deleted"
						a.statusErr = false
						a.reviewPanelView.Reload()
					}
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
			a.workspaceTicketID = id
			a.workspaceLines = nil
			a.workspaceRunning = true
			a.workspacePurpose = "dispatch"
			a.screen = screenPreparing
			return a, a.spawnWorkspaceDispatch(id)
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
			if err := a.wf.Delete(a.pendingDeleteID); err != nil {
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
			a.workspaceTicketID = id
			a.workspaceLines = nil
			a.workspaceRunning = true
			a.workspacePurpose = "redraft"
			a.screen = screenTearingDown
			return a, a.spawnWorkspaceRedraft(id)
		default:
			a.pendingRedraftID = ""
			a.screen = screenList
		}
	}
	return a, nil
}

// --- Edit draft message modal ---

func (a *App) updateEditDraftMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	returnTo := a.editDraftReturn
	if returnTo == 0 {
		returnTo = screenThreads
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = returnTo
			return a, nil
		case "ctrl+s":
			if a.editDraftModal != nil && a.editDraftModal.Text() != "" {
				if err := a.wf.UpdateDraftMessage(a.editDraftModal.MsgID(), a.editDraftModal.Text()); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = "Draft message updated"
					a.statusErr = false
					if a.threadsView != nil {
						a.threadsView.Reload()
					}
					if a.reviewPanelView != nil && returnTo == screenReviewPanel {
						a.reviewPanelView.Reload()
					}
				}
			}
			a.screen = returnTo
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
	returnTo := a.replyModalReturn
	if returnTo == 0 {
		returnTo = screenThreads
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = returnTo
			return a, nil
		case "ctrl+s":
			if a.replyModal.Text() != "" {
				ticketID := ""
				if a.threadsView != nil {
					ticketID = a.threadsView.TicketID()
				}
				if a.reviewPanelView != nil && returnTo == screenReviewPanel {
					ticketID = a.reviewPanelView.TicketID()
				}
				if _, err := a.wf.AddDraftMessage(a.replyModal.ThreadID(), ticketID, true, a.replyModal.Author(), a.replyModal.Text()); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = "Reply staged"
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
		case "ctrl+s":
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
				dt, err := a.wf.CreateDraftThread(ticketID, a.newThreadModal.TaskID(), filePath, hunkHeader)
				if err != nil {
					a.setErr(err)
					a.screen = returnTo
					return a, nil
				}
				if _, err := a.wf.AddDraftMessage(dt.ID, ticketID, false, a.newThreadModal.Author(), a.newThreadModal.Text()); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = "Thread staged (submit with [ctrl+s] in review panel)"
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
	case screenPreparing:
		sb.WriteString(a.renderWorkspaceScreen("preparing"))
	case screenTearingDown:
		sb.WriteString(a.renderWorkspaceScreen("tearing down"))
	case screenConfirmMarkMerged:
		prompt := fmt.Sprintf("Mark %s as merged? (y/N) ", a.pendingMarkMergedID)
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3")).Render(prompt))
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

// --- Workspace screens (preparing / tearing_down) ---

func (a *App) updateScreenPreparing(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		if km.String() == "esc" {
			a.screen = screenList
		}
	}
	return a, nil
}

func (a *App) updateScreenTearingDown(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		if km.String() == "esc" {
			a.screen = screenList
		}
	}
	return a, nil
}

// --- Confirm mark merged ---

func (a *App) updateConfirmMarkMerged(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "y", "Y":
			id := a.pendingMarkMergedID
			a.pendingMarkMergedID = ""
			if transErr := a.wf.TransitionTicket(id, model.StatusTearingDown); transErr != nil {
				a.setErr(transErr)
				a.screen = screenList
				return a, nil
			}
			a.workspaceTicketID = id
			a.workspaceLines = nil
			a.workspaceRunning = true
			a.workspacePurpose = "mark-merged"
			a.screen = screenTearingDown
			return a, a.spawnWorkspaceMarkMerged(id)
		default:
			a.pendingMarkMergedID = ""
			a.screen = screenList
		}
	}
	return a, nil
}

func (a *App) renderWorkspaceScreen(phase string) string {
	var sb strings.Builder
	headerColor := lipgloss.Color("8")
	if a.workspaceRunning {
		headerColor = lipgloss.Color("2")
	}
	header := lipgloss.NewStyle().Foreground(headerColor).Render(
		fmt.Sprintf("workspace: %s %s", phase, a.workspaceTicketID),
	)
	sb.WriteString(header + "\n\n")
	for _, line := range a.workspaceLines {
		sb.WriteString(line + "\n")
	}
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("esc: back"))
	return sb.String()
}

func (a *App) spawnWorkspaceDispatch(ticketID string) tea.Cmd {
	pr, pw := io.Pipe()
	var dispatchErr error
	go func() {
		dispatchErr = a.wf.Dispatch(ticketID, pw, pw)
		pw.Close()
	}()
	sc := bufio.NewScanner(pr)
	scanFn := func() tea.Msg {
		if sc.Scan() {
			return workspaceChunkMsg{line: sc.Text()}
		}
		return workspaceDoneMsg{err: dispatchErr}
	}
	a.workspaceScanNext = scanFn
	return scanFn
}

func (a *App) spawnWorkspaceRedraft(ticketID string) tea.Cmd {
	pr, pw := io.Pipe()
	var redraftErr error
	go func() {
		redraftErr = a.wf.Redraft(ticketID, pw, pw)
		pw.Close()
	}()
	sc := bufio.NewScanner(pr)
	scanFn := func() tea.Msg {
		if sc.Scan() {
			return workspaceChunkMsg{line: sc.Text()}
		}
		return workspaceDoneMsg{err: redraftErr}
	}
	a.workspaceScanNext = scanFn
	return scanFn
}

func (a *App) spawnWorkspaceMarkMerged(ticketID string) tea.Cmd {
	pr, pw := io.Pipe()
	var mergeErr error
	go func() {
		mergeErr = a.wf.MarkMerged(ticketID, pw, pw)
		pw.Close()
	}()
	sc := bufio.NewScanner(pr)
	scanFn := func() tea.Msg {
		if sc.Scan() {
			return workspaceChunkMsg{line: sc.Text()}
		}
		return workspaceDoneMsg{err: mergeErr}
	}
	a.workspaceScanNext = scanFn
	return scanFn
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
	sessions, err := a.wf.ListActiveAgentSessions()
	if err != nil {
		return
	}
	for _, sess := range sessions {
		if sess.State != model.AgentWaiting {
			continue
		}
		ticket, err := a.wf.GetTicket(sess.TicketID)
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
	sessions, err := a.wf.ListActiveAgentSessions()
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
