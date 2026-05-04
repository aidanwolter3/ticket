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
	ticket *model.Ticket
	indent int
}

type TicketsView struct {
	store  *store.Store
	rows   []ticketRow
	cursor int
	width  int
	height int
	err    error
}

func NewTicketsView(s *store.Store) *TicketsView {
	v := &TicketsView{store: s}
	v.load()
	return v
}

func (v *TicketsView) load() {
	tickets, err := v.store.ListTickets()
	if err != nil {
		v.err = err
		return
	}
	v.err = nil

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
	for _, t := range tickets {
		if !t.IsPlan() {
			continue
		}
		v.rows = append(v.rows, ticketRow{ticket: t, indent: 0})
		for _, childID := range t.BlockedBy {
			for _, child := range tickets {
				if child.ID == childID {
					v.rows = append(v.rows, ticketRow{ticket: child, indent: 1})
					break
				}
			}
		}
	}
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
		switch {
		case key.Matches(msg, upBinding):
			if v.cursor > 0 {
				v.cursor--
			}
		case key.Matches(msg, downBinding):
			if v.cursor < len(v.rows)-1 {
				v.cursor++
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

	if len(v.rows) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No tickets.") + "\n")
		return sb.String()
	}

	visible := v.height - 2
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
		maxTitle := v.width - row.indent*2 - len(row.ticket.ID) - 3
		if maxTitle < 5 {
			maxTitle = 5
		}
		if len([]rune(title)) > maxTitle {
			title = string([]rune(title)[:maxTitle-1]) + "…"
		}

		var line string
		if row.ticket.IsPlan() {
			line = fmt.Sprintf("%s%s %s %s",
				indent,
				lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("▼"),
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
		"↑↓/jk · d delete · tab tabs · q quit"))

	return sb.String()
}

var (
	upBinding   = key.NewBinding(key.WithKeys("up", "k"))
	downBinding = key.NewBinding(key.WithKeys("down", "j"))
)

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
