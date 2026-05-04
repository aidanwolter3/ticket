package components

import (
	"github.com/aidanwolter/ticket/internal/model"
	"github.com/charmbracelet/lipgloss"
)

var (
	colorYellow  = lipgloss.Color("3")
	colorGreen   = lipgloss.Color("2")
	colorWhite   = lipgloss.Color("15")
	colorGray    = lipgloss.Color("8")
	colorMagenta = lipgloss.Color("5")
)

func TicketStatusIcon(s model.Status) string {
	switch s {
	case model.StatusDraft:
		return lipgloss.NewStyle().Foreground(colorGray).Render("◌")
	case model.StatusReady:
		return lipgloss.NewStyle().Foreground(colorWhite).Render("○")
	case model.StatusInProgress:
		return lipgloss.NewStyle().Foreground(colorMagenta).Render("●")
	case model.StatusInReview:
		return lipgloss.NewStyle().Foreground(colorYellow).Render("◐")
	case model.StatusCompleted:
		return lipgloss.NewStyle().Foreground(colorGreen).Render("✓")
	default:
		return "?"
	}
}

func ThreadStatusIcon(s model.ThreadStatus) string {
	switch s {
	case model.ThreadActive:
		return lipgloss.NewStyle().Foreground(colorMagenta).Render("●")
	case model.ThreadReady:
		return lipgloss.NewStyle().Foreground(colorYellow).Render("◆")
	case model.ThreadResolved:
		return lipgloss.NewStyle().Foreground(colorGreen).Render("✓")
	default:
		return "?"
	}
}
