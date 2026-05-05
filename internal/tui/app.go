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

type appTab int

const (
	tabTickets appTab = iota
	tabReview
	tabDraft
)

type appScreen int

const (
	screenList appScreen = iota // split-pane: list left, detail right
	screenThreads
	screenStack
	screenForm
	screenStatusModal
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
	reviewView  *views.ReviewQueueView
	draftView   *views.DraftReviewView

	// detail views (right pane — always loaded for selected ticket)
	ticketDetail *views.TicketDetailView
	planDetail   *views.PlanDetailView

	// overlay screens
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
		reviewView:  views.NewReviewQueueView(s),
		draftView:   views.NewDraftReviewView(s),
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
	switch a.tab {
	case tabTickets:
		if t := a.ticketsView.SelectedTicket(); t != nil {
			return t.ID
		}
	case tabReview:
		return a.reviewView.FirstTicketID()
	case tabDraft:
		if t := a.draftView.SelectedTicket(); t != nil {
			return t.ID
		}
	}
	return ""
}

func (a *App) loadCurrentDetail() {
	a.loadDetailForID(a.selectedTicketID())
}

func (a *App) loadDetailForID(id string) {
	if id == "" {
		a.ticketDetail = nil
		a.planDetail = nil
		return
	}
	t, err := a.store.GetTicket(id)
	if err != nil {
		a.ticketDetail = nil
		a.planDetail = nil
		return
	}
	bodyH := a.bodyHeight()
	if t.IsPlan() {
		pd, err := views.NewPlanDetailView(a.store, id)
		if err != nil {
			return
		}
		pd.SetSize(a.rightW, bodyH)
		a.planDetail = pd
		a.ticketDetail = nil
	} else {
		td, err := views.NewTicketDetailView(a.store, id)
		if err != nil {
			return
		}
		td.SetSize(a.rightW, bodyH)
		a.ticketDetail = td
		a.planDetail = nil
	}
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
		a.reviewView.SetSize(a.leftW, bodyH)
		a.draftView.SetSize(a.leftW, bodyH)
		if a.ticketDetail != nil {
			a.ticketDetail.SetSize(a.rightW, bodyH)
		}
		if a.planDetail != nil {
			a.planDetail.SetSize(a.rightW, bodyH)
		}
		if a.threadsView != nil {
			a.threadsView.SetSize(a.width, a.height)
		}
		if a.stackView != nil {
			a.stackView.SetSize(a.width, a.height)
		}
		return a, nil

	case tea.KeyMsg:
		// Global shortcuts (only when not in a modal/form)
		if a.screen == screenList || a.screen == screenThreads ||
			a.screen == screenStack {
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
					prev := a.tab
					a.tab = (a.tab + 1) % 3
					if a.tab != prev {
						a.loadCurrentDetail()
					}
					return a, nil
				}
			case "shift+tab":
				if a.screen == screenList {
					prev := a.tab
					a.tab = (a.tab + 2) % 3
					if a.tab != prev {
						a.loadCurrentDetail()
					}
					return a, nil
				}
			case "1":
				if a.screen == screenList && a.tab != tabTickets {
					a.tab = tabTickets
					a.loadCurrentDetail()
					return a, nil
				}
			case "2":
				if a.screen == screenList && a.tab != tabReview {
					a.tab = tabReview
					a.loadCurrentDetail()
					return a, nil
				}
			case "3":
				if a.screen == screenList && a.tab != tabDraft {
					a.tab = tabDraft
					a.loadCurrentDetail()
					return a, nil
				}
			}
		}
	}

	switch a.screen {
	case screenList:
		return a.updateList(msg)
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

// --- List screen (split-pane) ---

func (a *App) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		// Detail-panel hotkeys — act on the currently highlighted ticket
		switch km.String() {
		case "e":
			id := a.currentTicketID()
			if id != "" {
				t, err := a.store.GetTicket(id)
				if err == nil {
					a.formView = views.NewFormView(t)
					a.formView.SetSize(a.width, a.height)
					a.screen = screenForm
				}
			}
			return a, nil
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
		case "s":
			id := a.currentTicketID()
			if id != "" && a.ticketDetail != nil {
				// Plans can't be manually transitioned
				a.statusModal = views.NewStatusModal(a.ticketDetail.Ticket().Status)
				a.screen = screenStatusModal
			}
			return a, nil
		case "S":
			id := a.currentTicketID()
			if id != "" {
				t, err := a.store.GetTicket(id)
				if err == nil && t.StackID != "" {
					sv, err := views.NewStackView(a.store, t.StackID, id)
					if err != nil {
						a.setErr(err)
						return a, nil
					}
					sv.SetSize(a.width, a.height)
					a.stackView = sv
					a.screen = screenStack
				}
			}
			return a, nil
		case "[":
			if a.ticketDetail != nil {
				a.ticketDetail.ScrollUp(3)
			} else if a.planDetail != nil {
				a.planDetail.ScrollUp(3)
			}
			return a, nil
		case "]":
			if a.ticketDetail != nil {
				a.ticketDetail.ScrollDown(3)
			} else if a.planDetail != nil {
				a.planDetail.ScrollDown(3)
			}
			return a, nil
		}

		// Tab-specific list actions
		switch a.tab {
		case tabTickets:
			switch km.String() {
			case "d":
				if t := a.ticketsView.SelectedTicket(); t != nil {
					a.pendingDeleteID = t.ID
					a.screen = screenConfirmDelete
				}
				return a, nil
			}
		case tabReview:
			switch km.String() {
			case "r":
				ids := a.reviewView.StackTicketIDs()
				if len(ids) > 0 {
					a.stackWalk = ids
					a.stackWalkIdx = 0
					a.loadDetailForID(ids[0])
				}
				return a, nil
			}
		case tabDraft:
			switch km.String() {
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
						a.loadCurrentDetail()
					}
				}
				return a, nil
			case "d":
				if t := a.draftView.SelectedTicket(); t != nil {
					a.pendingDeleteID = t.ID
					a.screen = screenConfirmDelete
				}
				return a, nil
			}
		}
	}

	// Delegate navigation to the active list view; reload detail on cursor change
	prevID := a.selectedTicketID()
	var cmd tea.Cmd
	switch a.tab {
	case tabTickets:
		_, cmd = a.ticketsView.Update(msg)
	case tabReview:
		_, cmd = a.reviewView.Update(msg)
	case tabDraft:
		_, cmd = a.draftView.Update(msg)
	}
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
				threads := a.threadsView.Threads()
				idx := a.threadsView.Cursor()
				if idx < len(threads) {
					a.replyModal = views.NewReplyModal(threads[idx].ID)
					a.screen = screenReplyModal
				}
			}
			return a, nil
		case "n":
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
					a.loadDetailForID(id)
					a.screen = screenList
				}
			}
			return a, nil
		case "r":
			if a.stackView != nil {
				ids := a.stackView.AllTicketIDs()
				if len(ids) > 0 {
					a.stackWalk = ids
					a.stackWalkIdx = 0
					a.loadDetailForID(ids[0])
					a.screen = screenList
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
			a.screen = screenList
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
			a.reloadCurrentDetail()
			a.screen = screenList
			return a, nil
		}
	}
	_, cmd := a.formView.Update(msg)
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
				a.draftView.Refresh()
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

// --- Status modal ---

func (a *App) updateStatusModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			a.screen = screenList
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
			a.screen = screenList
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
		a.reviewView.SetSize(a.leftW, bodyH)
		a.draftView.SetSize(a.leftW, bodyH)

		var leftContent string
		switch a.tab {
		case tabTickets:
			leftContent = a.ticketsView.View()
		case tabReview:
			leftContent = a.reviewView.View()
		case tabDraft:
			leftContent = a.draftView.View()
		}

		leftPane := lipgloss.NewStyle().
			Width(a.leftW).
			Height(bodyH).
			Render(leftContent)

		if a.ticketDetail != nil {
			a.ticketDetail.SetSize(a.rightW, bodyH)
		}
		if a.planDetail != nil {
			a.planDetail.SetSize(a.rightW, bodyH)
		}

		var rightContent string
		if a.ticketDetail != nil {
			rightContent = a.ticketDetail.View()
		} else if a.planDetail != nil {
			rightContent = a.planDetail.View()
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
			"1 / 2 / 3         jump to tab",
			"?                 toggle help",
			"q / ctrl+c        quit",
		}},
		{"List Panel (left)", []string{
			"↑↓ / j/k          navigate",
			"d                 delete ticket",
		}},
		{"Detail Panel (right)", []string{
			"e                 edit",
			"t                 threads",
			"n                 add note",
			"s                 change status",
			"S                 stack view",
			"[ / ]             scroll up / down",
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
			"r                 open stack (walk all)",
		}},
		{"Draft Review", []string{
			"↑↓                navigate",
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

func (a *App) reloadCurrentDetail() {
	if a.ticketDetail != nil {
		_ = a.ticketDetail.Reload()
	}
	if a.planDetail != nil {
		_ = a.planDetail.Reload()
	}
}
