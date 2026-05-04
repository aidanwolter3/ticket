package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/views"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type dbTickMsg struct{}

func tickDB() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return dbTickMsg{} })
}

type appTab int

const (
	tabTickets appTab = iota
	tabReview
	tabDraft
)

type appScreen int

const (
	screenList appScreen = iota
	screenTicketDetail
	screenPlanDetail
	screenThreads
	screenStack
	screenForm
	screenStatusModal
	screenNoteModal
	screenReplyModal
	screenNewThreadModal
	screenConfirmDelete
	screenHelp
)

type App struct {
	store           *store.Store
	tab             appTab
	screen          appScreen
	width           int
	height          int
	statusMsg       string
	statusErr       bool
	pendingDeleteID string

	// views
	ticketsView    *views.TicketsView
	reviewView     *views.ReviewQueueView
	draftView      *views.DraftReviewView
	ticketDetail   *views.TicketDetailView
	planDetail     *views.PlanDetailView
	threadsView    *views.ThreadsView
	stackView      *views.StackView
	formView       *views.FormView
	statusModal    *views.StatusModal
	noteModal      *views.NoteModal
	replyModal     *views.ReplyModal
	newThreadModal *views.NewThreadModal

	// stack review walk
	stackWalk    []string
	stackWalkIdx int

	showHelp bool
}

func New(s *store.Store) *App {
	return &App{
		store:       s,
		tab:         tabTickets,
		screen:      screenList,
		ticketsView: views.NewTicketsView(s),
		reviewView:  views.NewReviewQueueView(s),
		draftView:   views.NewDraftReviewView(s),
	}
}

func (a *App) Init() tea.Cmd {
	return tickDB()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case dbTickMsg:
		a.ticketsView.Refresh()
		a.reviewView.Refresh()
		a.draftView.Refresh()
		if a.ticketDetail != nil {
			_ = a.ticketDetail.Reload()
		}
		if a.planDetail != nil {
			_ = a.planDetail.Reload()
		}
		if a.threadsView != nil {
			_ = a.threadsView.Reload()
		}
		if a.stackView != nil {
			_ = a.stackView.Reload()
		}
		return a, tickDB()

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ticketsView.SetSize(msg.Width, msg.Height)
		a.reviewView.SetSize(msg.Width, msg.Height)
		a.draftView.SetSize(msg.Width, msg.Height)
		if a.ticketDetail != nil {
			a.ticketDetail.SetSize(msg.Width, msg.Height)
		}
		if a.planDetail != nil {
			a.planDetail.SetSize(msg.Width, msg.Height)
		}
		if a.threadsView != nil {
			a.threadsView.SetSize(msg.Width, msg.Height)
		}
		if a.stackView != nil {
			a.stackView.SetSize(msg.Width, msg.Height)
		}
		return a, nil

	case tea.KeyMsg:
		// Global shortcuts (only when not in a modal/form)
		if a.screen == screenList || a.screen == screenTicketDetail || a.screen == screenPlanDetail ||
			a.screen == screenThreads || a.screen == screenStack {
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
			case "tab":
				if a.screen == screenList {
					a.tab = (a.tab + 1) % 3
					return a, nil
				}
			case "shift+tab":
				if a.screen == screenList {
					a.tab = (a.tab + 2) % 3
					return a, nil
				}
			case "1":
				if a.screen == screenList {
					a.tab = tabTickets
					return a, nil
				}
			case "2":
				if a.screen == screenList {
					a.tab = tabReview
					return a, nil
				}
			case "3":
				if a.screen == screenList {
					a.tab = tabDraft
					return a, nil
				}
			}
		}
	}

	switch a.screen {
	case screenList:
		return a.updateList(msg)
	case screenTicketDetail:
		return a.updateTicketDetail(msg)
	case screenPlanDetail:
		return a.updatePlanDetail(msg)
	case screenThreads:
		return a.updateThreads(msg)
	case screenStack:
		return a.updateStack(msg)
	case screenForm:
		return a.updateForm(msg)
	case screenConfirmDelete:
		return a.updateConfirmDelete(msg)
	case screenStatusModal:
		return a.updateStatusModal(msg)
	case screenNoteModal:
		return a.updateNoteModal(msg)
	case screenReplyModal:
		return a.updateReplyModal(msg)
	case screenNewThreadModal:
		return a.updateNewThreadModal(msg)
	}
	return a, nil
}

// --- List screen ---

func (a *App) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		if a.tab == tabTickets {
			switch km.String() {
			case "enter":
				t := a.ticketsView.SelectedTicket()
				if t == nil {
					return a, nil
				}
				return a.openTicket(t.ID)
			case "n":
				a.formView = views.NewFormView(nil)
				a.formView.SetSize(a.width, a.height)
				a.screen = screenForm
				a.statusMsg = ""
				return a, nil
			case "e":
				t := a.ticketsView.SelectedTicket()
				if t == nil {
					return a, nil
				}
				a.formView = views.NewFormView(t)
				a.formView.SetSize(a.width, a.height)
				a.screen = screenForm
				return a, nil
			case "s":
				t := a.ticketsView.SelectedTicket()
				if t != nil && t.StackID != "" {
					sv, err := views.NewStackView(a.store, t.StackID, t.ID)
					if err != nil {
						a.setErr(err)
						return a, nil
					}
					sv.SetSize(a.width, a.height)
					a.stackView = sv
					a.screen = screenStack
				}
				return a, nil
			case "d":
				t := a.ticketsView.SelectedTicket()
				if t != nil {
					a.pendingDeleteID = t.ID
					a.screen = screenConfirmDelete
				}
				return a, nil
			}
			// Delegate remaining keys to tickets view
			_, cmd := a.ticketsView.Update(msg)
			return a, cmd
		} else if a.tab == tabReview {
			switch km.String() {
			case "enter":
				id := a.reviewView.FirstTicketID()
				if id != "" {
					return a.openTicket(id)
				}
			case "r":
				ids := a.reviewView.StackTicketIDs()
				if len(ids) > 0 {
					a.stackWalk = ids
					a.stackWalkIdx = 0
					return a.openTicket(ids[0])
				}
			}
			_, cmd := a.reviewView.Update(msg)
			return a, cmd
		} else {
			// Draft review tab
			switch km.String() {
			case "enter":
				t := a.draftView.SelectedTicket()
				if t != nil {
					return a.openTicket(t.ID)
				}
			case "a":
				t := a.draftView.SelectedTicket()
				if t != nil {
					if err := a.store.TransitionTicket(t.ID, "ready", "human"); err != nil {
						a.setErr(err)
					} else {
						a.statusMsg = fmt.Sprintf("%s → ready", t.ID)
						a.statusErr = false
						a.draftView.Refresh()
						a.ticketsView.Refresh()
					}
				}
				return a, nil
			case "d":
				t := a.draftView.SelectedTicket()
				if t != nil {
					a.pendingDeleteID = t.ID
					a.screen = screenConfirmDelete
				}
				return a, nil
			}
			_, cmd := a.draftView.Update(msg)
			return a, cmd
		}
	}
	// Pass non-key messages
	if a.tab == tabTickets {
		_, cmd := a.ticketsView.Update(msg)
		return a, cmd
	} else if a.tab == tabReview {
		_, cmd := a.reviewView.Update(msg)
		return a, cmd
	}
	_, cmd := a.draftView.Update(msg)
	return a, cmd
}

func (a *App) openTicket(id string) (tea.Model, tea.Cmd) {
	t, err := a.store.GetTicket(id)
	if err != nil {
		a.setErr(err)
		return a, nil
	}
	if t.IsPlan() {
		pd, err := views.NewPlanDetailView(a.store, id)
		if err != nil {
			a.setErr(err)
			return a, nil
		}
		pd.SetSize(a.width, a.height)
		a.planDetail = pd
		a.screen = screenPlanDetail
	} else {
		td, err := views.NewTicketDetailView(a.store, id)
		if err != nil {
			a.setErr(err)
			return a, nil
		}
		td.SetSize(a.width, a.height)
		a.ticketDetail = td
		a.screen = screenTicketDetail
	}
	return a, nil
}

// --- Ticket detail ---

func (a *App) updateTicketDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = screenList
			a.ticketsView.Refresh()
			a.reviewView.Refresh()
			a.draftView.Refresh()
			return a, nil
		case "e":
			t := a.ticketDetail.Ticket()
			if t != nil {
				a.formView = views.NewFormView(t)
				a.formView.SetSize(a.width, a.height)
				a.screen = screenForm
			}
			return a, nil
		case "t":
			t := a.ticketDetail.Ticket()
			if t != nil {
				tv, err := views.NewThreadsView(a.store, t.ID)
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
			a.noteModal = views.NewNoteModal()
			a.screen = screenNoteModal
			return a, nil
		case "s":
			t := a.ticketDetail.Ticket()
			if t != nil {
				a.statusModal = views.NewStatusModal(t.Status)
				a.screen = screenStatusModal
			}
			return a, nil
		case "S": // stack view
			t := a.ticketDetail.Ticket()
			if t != nil && t.StackID != "" {
				sv, err := views.NewStackView(a.store, t.StackID, t.ID)
				if err != nil {
					a.setErr(err)
					return a, nil
				}
				sv.SetSize(a.width, a.height)
				a.stackView = sv
				a.screen = screenStack
			}
			return a, nil
		}
	}
	_, cmd := a.ticketDetail.Update(msg)
	return a, cmd
}

// --- Plan detail ---

func (a *App) updatePlanDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = screenList
			a.ticketsView.Refresh()
			a.reviewView.Refresh()
			a.draftView.Refresh()
			return a, nil
		case "e":
			t := a.planDetail.Ticket()
			if t != nil {
				a.formView = views.NewFormView(t)
				a.formView.SetSize(a.width, a.height)
				a.screen = screenForm
			}
			return a, nil
		case "t":
			t := a.planDetail.Ticket()
			if t != nil {
				tv, err := views.NewThreadsView(a.store, t.ID)
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
			a.noteModal = views.NewNoteModal()
			a.screen = screenNoteModal
			return a, nil
		case "a":
			// Create child ticket linked to this plan
			if t := a.planDetail.Ticket(); t != nil {
				child := views.NewFormView(nil)
				child.SetSize(a.width, a.height)
				a.formView = child
				a.screen = screenForm
				a.statusMsg = fmt.Sprintf("Creating child for %s — set BlockedBy to %s", t.ID, t.ID)
			}
			return a, nil
		}
	}
	_, cmd := a.planDetail.Update(msg)
	return a, cmd
}

// --- Threads screen ---

func (a *App) updateThreads(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			// Return to appropriate detail view
			if a.ticketDetail != nil {
				a.ticketDetail.Reload()
				a.screen = screenTicketDetail
			} else if a.planDetail != nil {
				a.planDetail.Reload()
				a.screen = screenPlanDetail
			} else {
				a.screen = screenList
			}
			return a, nil
		case "r":
			// Reply to selected thread
			if a.threadsView != nil {
				threads := a.threadsView.Threads()
				idx := a.threadsView.Cursor()
				if idx < len(threads) {
					a.replyModal = views.NewReplyModal(threads[idx].ID)
					a.screen = screenReplyModal
				}
			}
			return a, nil
		case "n":
			// New thread
			if a.threadsView != nil {
				a.newThreadModal = views.NewNewThreadModal(a.threadsView.TicketID())
				a.screen = screenNewThreadModal
			}
			return a, nil
		}
	}
	_, cmd := a.threadsView.Update(msg)
	return a, cmd
}

// --- Stack screen ---

func (a *App) updateStack(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = screenList
			return a, nil
		case "enter":
			if a.stackView != nil {
				id := a.stackView.SelectedTicketID()
				if id != "" {
					return a.openTicket(id)
				}
			}
			return a, nil
		case "r":
			if a.stackView != nil {
				ids := a.stackView.AllTicketIDs()
				if len(ids) > 0 {
					a.stackWalk = ids
					a.stackWalkIdx = 0
					return a.openTicket(ids[0])
				}
			}
			return a, nil
		}
	}
	_, cmd := a.stackView.Update(msg)
	return a, cmd
}

// --- Form screen ---

func (a *App) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = a.prevDetailScreen()
			return a, nil
		case "ctrl+s":
			t := a.formView.ToTicket()
			if t.Title == "" {
				a.statusMsg = "Title is required"
				a.statusErr = true
				return a, nil
			}
			if a.formView.IsEdit() {
				existing, err := a.store.GetTicket(a.formView.TicketID())
				if err != nil {
					a.setErr(err)
					return a, nil
				}
				t.ID = existing.ID
				t.Created = existing.Created
				if err := a.store.UpdateTicket(t); err != nil {
					a.setErr(err)
					return a, nil
				}
				a.statusMsg = fmt.Sprintf("Updated %s", t.ID)
			} else {
				if err := a.store.CreateTicket(t); err != nil {
					a.setErr(err)
					return a, nil
				}
				a.statusMsg = fmt.Sprintf("Created %s", t.ID)
			}
			a.statusErr = false
			a.ticketsView.Refresh()
			a.reviewView.Refresh()
			a.screen = a.prevDetailScreen()
			return a, nil
		}
	}
	_, cmd := a.formView.Update(msg)
	return a, cmd
}

func (a *App) prevDetailScreen() appScreen {
	if a.ticketDetail != nil && a.screen != screenTicketDetail {
		a.ticketDetail.Reload()
		return screenTicketDetail
	}
	if a.planDetail != nil && a.screen != screenPlanDetail {
		a.planDetail.Reload()
		return screenPlanDetail
	}
	return screenList
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
				a.draftView.Refresh()
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

// --- Status modal ---

func (a *App) updateStatusModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = a.currentDetailScreen()
			return a, nil
		case "enter":
			to := a.statusModal.Selected()
			id := a.currentTicketID()
			if id != "" {
				if err := a.store.TransitionTicket(id, to, "human"); err != nil {
					a.setErr(err)
				} else {
					a.statusMsg = fmt.Sprintf("Status → %s", to)
					a.statusErr = false
					a.reloadCurrentDetail()
					a.ticketsView.Refresh()
					a.reviewView.Refresh()
				}
			}
			a.screen = a.currentDetailScreen()
			return a, nil
		}
	}
	_, cmd := a.statusModal.Update(msg)
	return a, cmd
}

// --- Note modal ---

func (a *App) updateNoteModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = a.currentDetailScreen()
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
				a.screen = a.currentDetailScreen()
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
				thread, err := a.store.CreateThread(a.newThreadModal.TicketID())
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

	// Tab bar
	tabBar := a.renderTabBar()
	sb.WriteString(tabBar + "\n")
	sb.WriteString(strings.Repeat("─", a.width) + "\n")

	bodyHeight := a.height - 3 // tab bar + divider + status
	if bodyHeight < 1 {
		bodyHeight = 20
	}

	// Help overlay
	if a.showHelp {
		sb.WriteString(a.renderHelp())
		return sb.String()
	}

	switch a.screen {
	case screenList:
		if a.tab == tabTickets {
			sb.WriteString(a.ticketsView.View())
		} else if a.tab == tabReview {
			sb.WriteString(a.reviewView.View())
		} else {
			sb.WriteString(a.draftView.View())
		}
	case screenTicketDetail:
		if a.ticketDetail != nil {
			sb.WriteString(a.ticketDetail.View())
		}
	case screenPlanDetail:
		if a.planDetail != nil {
			sb.WriteString(a.planDetail.View())
		}
	case screenThreads:
		if a.threadsView != nil {
			sb.WriteString(a.threadsView.View())
		}
	case screenStack:
		if a.stackView != nil {
			sb.WriteString(a.stackView.View())
		}
	case screenForm:
		if a.formView != nil {
			sb.WriteString(a.formView.View())
		}
	case screenStatusModal:
		if a.statusModal != nil {
			sb.WriteString(a.statusModal.View())
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

	// Status bar
	statusLine := a.statusMsg
	if a.statusErr {
		statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗ " + statusLine)
	} else if statusLine != "" {
		statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓ " + statusLine)
	}
	if statusLine != "" {
		// Pad to bottom
		sb.WriteString("\n" + statusLine)
	}

	return sb.String()
}

func (a *App) renderTabBar() string {
	tabs := []string{"1 Tickets", "2 Review Queue", "3 Draft Review"}
	var parts []string
	for i, label := range tabs {
		if appTab(i) == a.tab {
			parts = append(parts, lipgloss.NewStyle().Bold(true).Underline(true).Padding(0, 1).Render(label))
		} else {
			parts = append(parts, lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("8")).Render(label))
		}
	}
	return strings.Join(parts, "  ") + "   " +
		lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("? help")
}

func (a *App) renderHelp() string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Keybindings") + "\n\n")

	sections := []struct {
		title string
		lines []string
	}{
		{"Global", []string{
			"tab / shift+tab   switch tabs",
			"1 / 2             jump to tab",
			"?                 toggle help",
			"q / ctrl+c        quit",
		}},
		{"Tickets List", []string{
			"↑↓ / j/k          navigate",
			"enter             open ticket",
			"n                 new ticket",
			"e                 edit ticket",
			"d                 delete ticket",
			"space             expand/collapse plan",
			"f                 cycle filter",
			"/                 search",
			"s                 stack view",
		}},
		{"Ticket Detail", []string{
			"e                 edit",
			"t                 threads",
			"n                 add note",
			"s                 change status",
			"S                 stack view",
			"esc               back",
		}},
		{"Threads", []string{
			"↑↓                navigate",
			"enter             expand/collapse",
			"r                 reply",
			"→                 toggle active/ready",
			"x                 resolve",
			"←                 reopen",
			"n                 new thread",
			"esc               back",
		}},
		{"Review Queue", []string{
			"↑↓                navigate",
			"enter             open first ticket",
			"r                 review stack (walk all)",
		}},
		{"Draft Review", []string{
			"↑↓                navigate",
			"enter             open ticket",
			"a                 approve → ready",
			"d                 delete ticket",
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

func (a *App) currentTicketID() string {
	if a.ticketDetail != nil && a.ticketDetail.Ticket() != nil {
		return a.ticketDetail.Ticket().ID
	}
	if a.planDetail != nil && a.planDetail.Ticket() != nil {
		return a.planDetail.Ticket().ID
	}
	return ""
}

func (a *App) currentDetailScreen() appScreen {
	if a.ticketDetail != nil {
		return screenTicketDetail
	}
	if a.planDetail != nil {
		return screenPlanDetail
	}
	return screenList
}

func (a *App) reloadCurrentDetail() {
	if a.ticketDetail != nil {
		a.ticketDetail.Reload()
	}
	if a.planDetail != nil {
		a.planDetail.Reload()
	}
}
