package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	barFilled        = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("█")
	barDash          = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("─")
	barTerminusLeft  = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("├")
	barTerminusRight = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("┤")
)

func ProgressBar(completed, total, width int) string {
	if total == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("no children")
	}
	pct := float64(completed) / float64(total)
	filled := int(pct * float64(width))
	empty := width - filled
	var bar string
	if filled == 0 {
		bar = barTerminusLeft + strings.Repeat(barDash, empty-1) + barTerminusRight
	} else if empty > 0 {
		bar = strings.Repeat(barFilled, filled+1) + strings.Repeat(barDash, empty-1) + barTerminusRight
	} else {
		bar = strings.Repeat(barFilled, filled+1)
	}
	return fmt.Sprintf("%s %d/%d", bar, completed, total)
}
