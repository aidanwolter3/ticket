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

type StackView struct {
	store     *store.Store
	stackID   string
	tickets   []*model.Ticket
	cursor    int
	highlight string // ticket ID to highlight
	width     int
	height    int
	err       error
}

func NewStackView(s *store.Store, stackID, highlightID string) (*StackView, error) {
	v := &StackView{store: s, stackID: stackID, highlight: highlightID}
	return v, v.load()
}

func (v *StackView) load() error {
	all, err := v.store.ListTickets()
	if err != nil {
		return err
	}
	v.tickets = nil
	for _, t := range all {
		if t.StackID == v.stackID {
			v.tickets = append(v.tickets, t)
		}
	}
	// Set cursor to highlighted ticket
	for i, t := range v.tickets {
		if t.ID == v.highlight {
			v.cursor = i
			break
		}
	}
	return nil
}

func (v *StackView) Reload() error       { return v.load() }
func (v *StackView) SetSize(w, h int) { v.width = w; v.height = h }
func (v *StackView) Init() tea.Cmd    { return nil }

func (v *StackView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (v *StackView) SelectedTicketID() string {
	if v.cursor < len(v.tickets) {
		return v.tickets[v.cursor].ID
	}
	return ""
}

func (v *StackView) AllTicketIDs() []string {
	ids := make([]string, len(v.tickets))
	for i, t := range v.tickets {
		ids[i] = t.ID
	}
	return ids
}

func (v *StackView) View() string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")).Render(
		fmt.Sprintf("Stack: %s", v.stackID)) + "\n\n")

	if len(v.tickets) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No tickets in this stack.") + "\n")
		return sb.String()
	}

	// Stack health
	allInReview := true
	for _, t := range v.tickets {
		if t.Status != model.StatusInReview {
			allInReview = false
			break
		}
	}
	if allInReview {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("◆ Ready for review") + "\n\n")
	}

	for i, t := range v.tickets {
		icon := components.TicketStatusIcon(t.Status)
		line := fmt.Sprintf("%s %s  %s",
			icon,
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(t.ID),
			t.Title)
		if i == v.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		if t.ID == v.highlight {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"↑↓ navigate · enter open ticket · r review all · esc back"))

	return sb.String()
}
