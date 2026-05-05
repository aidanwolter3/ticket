package views

import (
	"fmt"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	comp "github.com/aidanwolter/ticket/internal/tui/components"
)

type DraftReviewView struct {
	store   *store.Store
	tickets []*model.Ticket
	cursor  int
	width   int
	height  int
	err     error
}

func NewDraftReviewView(s *store.Store) *DraftReviewView {
	v := &DraftReviewView{store: s}
	v.load()
	return v
}

func (v *DraftReviewView) load() {
	q, err := v.store.DraftQueue()
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

func (v *DraftReviewView) Refresh()         { v.load() }
func (v *DraftReviewView) SetSize(w, h int) { v.width = w; v.height = h }
func (v *DraftReviewView) Init() tea.Cmd    { return nil }

func (v *DraftReviewView) SelectedTicket() *model.Ticket {
	if v.cursor >= len(v.tickets) {
		return nil
	}
	return v.tickets[v.cursor]
}

func (v *DraftReviewView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (v *DraftReviewView) View() string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Draft Review") + "\n\n")

	if v.err != nil {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Error: "+v.err.Error()) + "\n")
		return sb.String()
	}

	if len(v.tickets) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No draft tickets pending review.") + "\n")
		return sb.String()
	}

	visible := v.height - 6
	if visible < 1 {
		visible = len(v.tickets)
	}
	start := 0
	if v.cursor >= visible {
		start = v.cursor - visible + 1
	}

	for i := start; i < len(v.tickets) && i < start+visible; i++ {
		t := v.tickets[i]
		icon := components.TicketStatusIcon(t.Status)

		taskCount := len(t.Tasks)
		taskStr := ""
		if taskCount > 0 {
			taskStr = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
				Render(fmt.Sprintf("%d task(s)", taskCount))
		}

		var progressStr string
		if taskCount > 0 {
			progressStr = " " + comp.ProgressBar(0, taskCount, 10)
		}

		title := t.Title
		if len(title) > 50 {
			title = title[:50] + "…"
		}
		line := fmt.Sprintf("%s %s  %s%s%s",
			icon,
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(t.ID),
			title,
			taskStr,
			progressStr,
		)
		if i == v.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"↑↓ navigate · enter open · r promote → ready · d delete · q quit"))

	return sb.String()
}
