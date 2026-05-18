package views

import (
	"fmt"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/tui/components"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ticketsStore is the minimal store surface needed by TicketsView.
type ticketsStore interface {
	ListTickets(filter ...model.Status) ([]*model.Ticket, error)
	ListBacklogTickets() ([]*model.Ticket, error)
	GetAgentSessionByTicket(ticketID string) (*model.AgentSession, error)
}

type TicketsView struct {
	store            ticketsStore
	tickets          []*model.Ticket
	agentSessions    map[string]*model.AgentSession // ticketID → active session
	hideMerged       bool
	showBacklog      bool
	agentFocused     bool
	isCustomWorkspace bool
	cursor           int
	width            int
	height           int
	err              error
}

// SetIsCustomWorkspace updates the cached custom-workspace flag.
func (v *TicketsView) SetIsCustomWorkspace(custom bool) { v.isCustomWorkspace = custom }

func NewTicketsView(s ticketsStore) *TicketsView {
	v := &TicketsView{store: s, hideMerged: true}
	v.load()
	return v
}

func (v *TicketsView) load() {
	var tickets []*model.Ticket
	var err error
	if v.showBacklog {
		tickets, err = v.store.ListBacklogTickets()
	} else {
		tickets, err = v.store.ListTickets()
	}
	if err != nil {
		v.err = err
		return
	}
	v.err = nil
	v.tickets = tickets

	// Load agent sessions for all tickets.
	sessions := make(map[string]*model.AgentSession, len(tickets))
	for _, t := range tickets {
		sess, err := v.store.GetAgentSessionByTicket(t.ID)
		if err == nil && sess != nil {
			sessions[t.ID] = sess
		}
	}
	v.agentSessions = sessions

	if v.cursor >= len(v.visible()) {
		v.cursor = max(0, len(v.visible())-1)
	}
}

func (v *TicketsView) visible() []*model.Ticket {
	if !v.hideMerged {
		return v.tickets
	}
	filtered := make([]*model.Ticket, 0, len(v.tickets))
	for _, t := range v.tickets {
		if t.Status != model.StatusMerged {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (v *TicketsView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

func (v *TicketsView) SetAgentFocused(focused bool) {
	v.agentFocused = focused
}

func (v *TicketsView) Refresh() {
	v.load()
}

func (v *TicketsView) SelectedTicket() *model.Ticket {
	vis := v.visible()
	if len(vis) == 0 || v.cursor >= len(vis) {
		return nil
	}
	return vis[v.cursor]
}

// VisibleTickets returns the currently visible (non-filtered) ticket list in display order.
func (v *TicketsView) VisibleTickets() []*model.Ticket {
	return v.visible()
}

// SelectTicketByID sets the cursor to the ticket with the given ID.
// Returns true if the ticket was found and selected.
func (v *TicketsView) SelectTicketByID(id string) bool {
	vis := v.visible()
	for i, t := range vis {
		if t.ID == id {
			v.cursor = i
			return true
		}
	}
	return false
}

func (v *TicketsView) Init() tea.Cmd { return nil }

func (v *TicketsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, upBinding):
			if v.cursor > 0 {
				v.cursor--
			}
		case key.Matches(msg, downBinding):
			if v.cursor < len(v.visible())-1 {
				v.cursor++
			}
		case key.Matches(msg, toggleMergedBinding):
			v.hideMerged = !v.hideMerged
			if v.cursor >= len(v.visible()) {
				v.cursor = max(0, len(v.visible())-1)
			}
		case key.Matches(msg, toggleBacklogBinding):
			v.showBacklog = !v.showBacklog
			v.load()
			if v.cursor >= len(v.visible()) {
				v.cursor = max(0, len(v.visible())-1)
			}
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	}
	return v, nil
}

func (v *TicketsView) View() string {
	var sb strings.Builder

	if v.err != nil {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Error: "+v.err.Error()) + "\n")
		return sb.String()
	}

	tickets := v.visible()

	if len(tickets) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No tickets.") + "\n")
		return sb.String()
	}

	visible := v.height - 2
	if visible < 1 {
		visible = len(tickets)
	}
	start := 0
	if v.cursor >= visible {
		start = v.cursor - visible + 1
	}

	// Compute max ID length and max task count for column alignment.
	maxIDLen := 0
	maxTaskCount := 0
	for _, t := range tickets {
		if l := len(t.ID); l > maxIDLen {
			maxIDLen = l
		}
		if l := len(t.Tasks); l > maxTaskCount {
			maxTaskCount = l
		}
	}

	// Reserve a fixed right column for the progress bar when any ticket has tasks.
	// Bar format: "├████████ N/M" — left terminus + blocks fixed width=8 + space + fraction.
	barColWidth := 0
	maxFracWidth := 0
	if maxTaskCount > 0 {
		maxFracWidth = len(fmt.Sprintf("%d/%d", maxTaskCount, maxTaskCount))
		barColWidth = 1 + 8 + 1 + maxFracWidth
	}

	// Layout: cursor(1) SP agentPrefix(2) icon(1) SP id(maxIDLen) SP title(titleWidth) [SP bar(barColWidth)]
	titleWidth := v.width - 7 - maxIDLen
	if barColWidth > 0 {
		titleWidth -= 1 + barColWidth
	}
	if titleWidth < 5 {
		titleWidth = 5
	}

	ticketStatus := make(map[string]model.Status, len(v.tickets))
	for _, t := range v.tickets {
		ticketStatus[t.ID] = t.Status
	}

	for i := start; i < len(tickets) && i < start+visible; i++ {
		t := tickets[i]
		icon := components.TicketStatusIcon(t.Status)
		for _, blockerID := range t.BlockedBy {
			if s := ticketStatus[blockerID]; s != model.StatusMerged {
				icon = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B")).Render("●")
				break
			}
		}

		// Agent indicator prefix.
		agentPrefix := "  "
		if sess, ok := v.agentSessions[t.ID]; ok {
			switch sess.State {
			case model.AgentRunning:
				agentPrefix = "⚙ "
			case model.AgentWaiting:
				agentPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("? ") + ""
			}
		}

		// Truncate title then right-pad to titleWidth so bars stay column-aligned.
		titleRunes := []rune(t.Title)
		title := t.Title
		if len(titleRunes) > titleWidth {
			title = string(titleRunes[:titleWidth-1]) + "…"
			titleRunes = []rune(title)
		}
		title += strings.Repeat(" ", titleWidth-len(titleRunes))

		// Build progress bar with a fixed-width fraction for column alignment.
		barStr := ""
		if barColWidth > 0 {
			taskCount := len(t.Tasks)
			if taskCount > 0 {
				done := 0
				for _, task := range t.Tasks {
					if task.CompletedAt != nil {
						done++
					}
				}
				fracLen := len(fmt.Sprintf("%d/%d", done, taskCount))
				barStr = " " + components.ProgressBar(done, taskCount, 8) + strings.Repeat(" ", maxFracWidth-fracLen)
			} else {
				barStr = " " + strings.Repeat(" ", barColWidth)
			}
		}

		merged := t.Status == "merged"
		idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6CB6FF"))
		titleStyle := lipgloss.NewStyle()
		if merged || (v.agentFocused && i != v.cursor) {
			titleStyle = titleStyle.Foreground(lipgloss.Color("8"))
		}

		// Pad ID before styling so ANSI codes don't skew column width.
		paddedID := fmt.Sprintf("%-*s", maxIDLen, t.ID)

		cursor := " "
		if i == v.cursor {
			cursor = ">"
		}

		line := fmt.Sprintf("%s %s%s %s %s%s",
			cursor,
			agentPrefix,
			icon,
			idStyle.Render(paddedID),
			titleStyle.Render(title),
			barStr,
		)
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")
	if v.agentFocused {
		agentHint := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true).Render("● agent focused") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  ctrl+] to detach")
		sb.WriteString(agentHint)
	} else {
		mergedHint := "[h] show merged"
		if !v.hideMerged {
			mergedHint = "[h] hide merged"
		}
		backlogHint := "[B] show backlog"
		if v.showBacklog {
			backlogHint = "[B] hide backlog"
		}
		backlogToggleHint := ""
		if sel := v.SelectedTicket(); sel != nil && sel.Status == model.StatusDraft {
			if sel.Backlog {
				backlogToggleHint = " · [b] unbacklog"
			} else {
				backlogToggleHint = " · [b] backlog"
			}
		}
		markMergedHint := ""
		if v.isCustomWorkspace {
			if sel := v.SelectedTicket(); sel != nil && sel.Status == model.StatusApproved {
				markMergedHint = " · [M] mark merged"
			}
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
			"[↑↓/jk] · [D] delete · " + mergedHint + " · " + backlogHint + backlogToggleHint + markMergedHint))
	}

	return sb.String()
}

var (
	upBinding            = key.NewBinding(key.WithKeys("up", "k"))
	downBinding          = key.NewBinding(key.WithKeys("down", "j"))
	toggleMergedBinding  = key.NewBinding(key.WithKeys("h"))
	toggleBacklogBinding = key.NewBinding(key.WithKeys("B"))
)

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
