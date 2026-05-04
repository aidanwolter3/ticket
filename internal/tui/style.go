package tui

import "github.com/charmbracelet/lipgloss"

var (
	ColorCyan    = lipgloss.Color("6")
	ColorWhite   = lipgloss.Color("15")
	ColorYellow  = lipgloss.Color("3")
	ColorGreen   = lipgloss.Color("2")
	ColorRed     = lipgloss.Color("1")
	ColorMagenta = lipgloss.Color("5")
	ColorGray    = lipgloss.Color("8")
	ColorBlue    = lipgloss.Color("4")

	StylePlan      = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleTicket    = lipgloss.NewStyle().Foreground(ColorWhite)
	StyleInReview  = lipgloss.NewStyle().Foreground(ColorYellow)
	StyleCompleted = lipgloss.NewStyle().Foreground(ColorGreen)
	StyleBlocked   = lipgloss.NewStyle().Foreground(ColorRed).Faint(true)
	StyleStack     = lipgloss.NewStyle().Foreground(ColorMagenta)
	StyleMuted     = lipgloss.NewStyle().Foreground(ColorGray)
	StyleSelected  = lipgloss.NewStyle().Reverse(true)
	StyleError     = lipgloss.NewStyle().Foreground(ColorRed).Bold(true)
	StyleBold      = lipgloss.NewStyle().Bold(true)
	StyleTab       = lipgloss.NewStyle().Padding(0, 2)
	StyleActiveTab = lipgloss.NewStyle().Padding(0, 2).Bold(true).Underline(true)
	StyleBorder    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	StyleTitle     = lipgloss.NewStyle().Bold(true).Foreground(ColorBlue)
)
