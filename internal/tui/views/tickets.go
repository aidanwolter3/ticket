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

type TicketsView struct {
	store      *store.Store
	tickets    []*model.Ticket
	hideMerged bool
	cursor     int
	width      int
	height     int
	err        error
}

func NewTicketsView(s *store.Store) *TicketsView {
	v := &TicketsView{store: s, hideMerged: true}
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
	v.tickets = tickets

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

	for i := start; i < len(tickets) && i < start+visible; i++ {
		t := tickets[i]
		icon := components.TicketStatusIcon(t.Status)

		taskCount := len(t.Tasks)
		taskTag := ""
		if taskCount > 0 {
			done := 0
			for _, task := range t.Tasks {
				if task.CompletedAt != nil {
					done++
				}
			}
			taskTag = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
				Render(fmt.Sprintf("[%d/%d]", done, taskCount))
		}

		maxTitle := v.width - len(t.ID) - 3
		if maxTitle < 5 {
			maxTitle = 5
		}
		title := t.Title
		if len([]rune(title)) > maxTitle {
			title = string([]rune(title)[:maxTitle-1]) + "…"
		}

		merged := t.Status == "merged"
		idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		titleStyle := lipgloss.NewStyle()
		if merged {
			titleStyle = titleStyle.Foreground(lipgloss.Color("8"))
		}

		line := fmt.Sprintf("%s %s %s%s",
			icon,
			idStyle.Render(t.ID),
			titleStyle.Render(title),
			taskTag,
		)

		if i == v.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")
	mergedHint := "[h] show merged"
	if !v.hideMerged {
		mergedHint = "[h] hide merged"
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"[↑↓/jk] · [d] delete · " + mergedHint))

	return sb.String()
}

var (
	upBinding            = key.NewBinding(key.WithKeys("up", "k"))
	downBinding          = key.NewBinding(key.WithKeys("down", "j"))
	toggleMergedBinding  = key.NewBinding(key.WithKeys("h"))
)

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
