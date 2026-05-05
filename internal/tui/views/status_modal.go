package views

import (
	"fmt"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var allStatuses = []model.Status{
	model.StatusDraft,
	model.StatusReady,
	model.StatusInProgress,
	model.StatusInReview,
	model.StatusApproved,
	model.StatusMerged,
}

type StatusModal struct {
	current model.Status
	cursor  int
	err     string
}

func NewStatusModal(current model.Status) *StatusModal {
	m := &StatusModal{current: current}
	for i, s := range allStatuses {
		if s == current {
			m.cursor = i
			break
		}
	}
	return m
}

func (m *StatusModal) Init() tea.Cmd { return nil }

func (m *StatusModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(allStatuses)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m *StatusModal) Selected() model.Status {
	return allStatuses[m.cursor]
}

func (m *StatusModal) View() string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Change Status") + "\n\n")
	for i, s := range allStatuses {
		icon := components.TicketStatusIcon(s)
		line := fmt.Sprintf("  %s %s", icon, string(s))
		if i == m.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		if s == m.current {
			line += lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(" (current)")
		}
		sb.WriteString(line + "\n")
	}
	if m.err != "" {
		sb.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(m.err) + "\n")
	}
	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("↑↓ select · enter confirm · esc cancel"))
	return sb.String()
}
