package views

import (
	"fmt"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/components"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ticketRow struct {
	ticket   *model.Ticket
	indent   int
	expanded bool // only for plans
}

type TicketsView struct {
	store       *store.Store
	rows        []ticketRow
	cursor      int
	filter      filterMode
	search      string
	searching   bool
	searchInput string
	width       int
	height      int
	err         error
}

type filterMode int

const (
	filterAll filterMode = iota
	filterDraft
	filterReady
	filterInProgress
	filterInReview
	filterCompleted
)

var filterLabels = []string{"all", "draft", "ready", "in_progress", "in_review", "completed"}
var filterStatuses = [][]model.Status{
	nil,
	{model.StatusDraft},
	{model.StatusReady},
	{model.StatusInProgress},
	{model.StatusInReview},
	{model.StatusCompleted},
}

func NewTicketsView(s *store.Store) *TicketsView {
	v := &TicketsView{store: s}
	v.load()
	return v
}

func (v *TicketsView) load() {
	statuses := filterStatuses[v.filter]
	var tickets []*model.Ticket
	var err error
	if len(statuses) == 0 {
		tickets, err = v.store.ListTickets()
	} else {
		tickets, err = v.store.ListTickets(statuses...)
	}
	if err != nil {
		v.err = err
		return
	}
	v.err = nil

	// Apply search filter
	if v.searchInput != "" {
		query := strings.ToLower(v.searchInput)
		var filtered []*model.Ticket
		for _, t := range tickets {
			if strings.Contains(strings.ToLower(t.Title), query) ||
				strings.Contains(strings.ToLower(t.Description), query) {
				filtered = append(filtered, t)
			}
		}
		tickets = filtered
	}

	// Build row list: plans first with children, then standalones
	planMap := make(map[string]*model.Ticket)
	for _, t := range tickets {
		if t.IsPlan() {
			planMap[t.ID] = t
		}
	}

	childSet := make(map[string]bool)
	for _, t := range tickets {
		if t.IsPlan() {
			for _, c := range t.BlockedBy {
				childSet[c] = true
			}
		}
	}

	v.rows = nil
	// Plans first
	for _, t := range tickets {
		if !t.IsPlan() {
			continue
		}
		expanded := true
		// preserve expansion state
		for _, r := range v.rows {
			if r.ticket.ID == t.ID {
				expanded = r.expanded
				break
			}
		}
		v.rows = append(v.rows, ticketRow{ticket: t, indent: 0, expanded: expanded})
		if expanded {
			for _, childID := range t.BlockedBy {
				for _, child := range tickets {
					if child.ID == childID {
						v.rows = append(v.rows, ticketRow{ticket: child, indent: 1})
						break
					}
				}
			}
		}
	}
	// Standalone tickets
	for _, t := range tickets {
		if t.IsPlan() || childSet[t.ID] {
			continue
		}
		v.rows = append(v.rows, ticketRow{ticket: t, indent: 0})
	}

	if v.cursor >= len(v.rows) {
		v.cursor = max(0, len(v.rows)-1)
	}
}

func (v *TicketsView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

func (v *TicketsView) Refresh() {
	v.load()
}

func (v *TicketsView) SelectedTicket() *model.Ticket {
	if len(v.rows) == 0 || v.cursor >= len(v.rows) {
		return nil
	}
	return v.rows[v.cursor].ticket
}

func (v *TicketsView) Init() tea.Cmd { return nil }

func (v *TicketsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if v.searching {
			return v.updateSearch(msg)
		}
		switch {
		case key.Matches(msg, upBinding):
			if v.cursor > 0 {
				v.cursor--
			}
		case key.Matches(msg, downBinding):
			if v.cursor < len(v.rows)-1 {
				v.cursor++
			}
		case key.Matches(msg, spaceBinding):
			if v.cursor < len(v.rows) && v.rows[v.cursor].ticket.IsPlan() {
				v.toggleExpand(v.cursor)
			}
		case key.Matches(msg, filterBinding):
			v.filter = filterMode((int(v.filter) + 1) % len(filterLabels))
			v.load()
		case key.Matches(msg, searchBinding):
			v.searching = true
			v.search = ""
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	}
	return v, nil
}

func (v *TicketsView) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		v.searching = false
		v.searchInput = v.search
		v.load()
	case "backspace":
		if len(v.search) > 0 {
			v.search = v.search[:len(v.search)-1]
		}
	default:
		if len(msg.String()) == 1 {
			v.search += msg.String()
		}
	}
	return v, nil
}

func (v *TicketsView) toggleExpand(idx int) {
	v.rows[idx].expanded = !v.rows[idx].expanded
	v.load()
	// restore cursor to the plan row
	for i, r := range v.rows {
		if r.ticket.ID == v.rows[idx].ticket.ID {
			v.cursor = i
			break
		}
	}
}

func (v *TicketsView) View() string {
	var sb strings.Builder

	// Filter bar
	filterBar := "Filter: "
	for i, label := range filterLabels {
		if i == int(v.filter) {
			filterBar += lipgloss.NewStyle().Bold(true).Underline(true).Render("[" + label + "]")
		} else {
			filterBar += "[" + label + "]"
		}
		filterBar += " "
	}
	if v.searching {
		filterBar += " Search: " + v.search + "█"
	} else if v.searchInput != "" {
		filterBar += " Search: " + lipgloss.NewStyle().Italic(true).Render(v.searchInput)
	}
	sb.WriteString(filterBar + "\n\n")

	if v.err != nil {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Error: "+v.err.Error()) + "\n")
		return sb.String()
	}

	if len(v.rows) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No tickets. Press 'n' to create one.") + "\n")
		return sb.String()
	}

	visible := v.height - 5
	if visible < 1 {
		visible = len(v.rows)
	}
	start := 0
	if v.cursor >= visible {
		start = v.cursor - visible + 1
	}

	for i := start; i < len(v.rows) && i < start+visible; i++ {
		row := v.rows[i]
		indent := strings.Repeat("  ", row.indent)
		icon := components.TicketStatusIcon(row.ticket.Status)
		title := row.ticket.Title
		if len(title) > 50 {
			title = title[:50] + "…"
		}

		var line string
		if row.ticket.IsPlan() {
			arrow := "▶ "
			if row.expanded {
				arrow = "▼ "
			}
			line = fmt.Sprintf("%s%s%s %s %s",
				indent,
				lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(arrow),
				icon,
				lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true).Render(row.ticket.ID),
				lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(title),
			)
		} else {
			stackTag := ""
			if row.ticket.StackID != "" {
				stackTag = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render("["+row.ticket.StackID+"]")
			}
			line = fmt.Sprintf("%s%s %s %s%s",
				indent, icon,
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(row.ticket.ID),
				title,
				stackTag,
			)
		}

		if i == v.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"↑↓ navigate · enter open · n new · e edit · space expand · f filter · / search · s stack · q quit"))

	return sb.String()
}

var (
	upBinding     = key.NewBinding(key.WithKeys("up", "k"))
	downBinding   = key.NewBinding(key.WithKeys("down", "j"))
	spaceBinding  = key.NewBinding(key.WithKeys(" "))
	filterBinding = key.NewBinding(key.WithKeys("f"))
	searchBinding = key.NewBinding(key.WithKeys("/"))
)

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
