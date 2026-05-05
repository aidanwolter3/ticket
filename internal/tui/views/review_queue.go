package views

import (
	"fmt"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ReviewQueueView struct {
	store   *store.Store
	tickets []*model.Ticket
	cursor  int
	width   int
	height  int
	err     error
	inlineErr string
}

func NewReviewQueueView(s *store.Store) *ReviewQueueView {
	v := &ReviewQueueView{store: s}
	v.load()
	return v
}

func (v *ReviewQueueView) load() {
	q, err := v.store.ReviewQueue()
	if err != nil {
		v.err = err
		return
	}
	v.err = nil
	v.tickets = q.Tickets
	if v.cursor >= len(v.tickets) && len(v.tickets) > 0 {
		v.cursor = len(v.tickets) - 1
	}
}

func (v *ReviewQueueView) Refresh()           { v.load() }
func (v *ReviewQueueView) ClearInlineErr()    { v.inlineErr = "" }
func (v *ReviewQueueView) SetInlineErr(s string) { v.inlineErr = s }

func (v *ReviewQueueView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

func (v *ReviewQueueView) SelectedTicket() *model.Ticket {
	if len(v.tickets) == 0 || v.cursor >= len(v.tickets) {
		return nil
	}
	return v.tickets[v.cursor]
}

func (v *ReviewQueueView) FirstTicketID() string {
	if t := v.SelectedTicket(); t != nil {
		return t.ID
	}
	return ""
}

func (v *ReviewQueueView) Init() tea.Cmd { return nil }

func (v *ReviewQueueView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if v.cursor > 0 {
				v.cursor--
			}
		case "down", "j":
			if v.cursor < len(v.tickets)-1 {
				v.cursor++
			}
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	}
	return v, nil
}

func (v *ReviewQueueView) View() string {
	var sb strings.Builder

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Review Queue") + "\n\n")

	if v.err != nil {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Error: "+v.err.Error()) + "\n")
		return sb.String()
	}

	if len(v.tickets) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No tickets pending review.") + "\n")
		return sb.String()
	}

	visible := v.height - 4
	if visible < 1 {
		visible = len(v.tickets)
	}
	start := 0
	if v.cursor >= visible {
		start = v.cursor - visible + 1
	}

	for i := start; i < len(v.tickets) && i < start+visible; i++ {
		t := v.tickets[i]

		active, ready := threadCountsForTicket(t)
		threadSummary := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
			fmt.Sprintf("%d active · %d ready threads", active, ready))

		title := t.Title
		if len([]rune(title)) > 40 {
			title = string([]rune(title)[:39]) + "…"
		}

		line := fmt.Sprintf("%s %s  %s  %s",
			components.TicketStatusIcon(t.Status),
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(t.ID),
			title,
			threadSummary,
		)
		if i == v.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")
	if v.inlineErr != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗ "+v.inlineErr) + "\n")
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"↑↓ navigate · enter open · A approve · q quit"))

	return sb.String()
}

func threadCountsForTicket(t *model.Ticket) (active, ready int) {
	for _, task := range t.Tasks {
		for _, th := range task.Threads {
			switch th.Status {
			case model.ThreadActive:
				active++
			case model.ThreadReady:
				ready++
			}
		}
	}
	return
}
