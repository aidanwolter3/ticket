package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	barFilled   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("█")
	barDash     = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("─")
	barTerminus = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("┤")
)

func ProgressBar(completed, total, width int) string {
	if total == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("no children")
	}
	pct := float64(completed) / float64(total)
	filled := int(pct * float64(width))
	empty := width - filled
	var bar string
	if empty > 0 {
		bar = strings.Repeat(barFilled, filled) + strings.Repeat(barDash, empty-1) + barTerminus
	} else {
		bar = strings.Repeat(barFilled, filled)
	}
	return fmt.Sprintf("%s %d/%d", bar, completed, total)
}
