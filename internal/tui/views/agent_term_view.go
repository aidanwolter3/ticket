package views

import (
	"fmt"
	"strings"
)

const (
	ansiReset = "\033[0m"
	ansiGreen = "\033[32m"
	ansiGray  = "\033[90m"
	ansiRed   = "\033[31m"
)

// AgentTermView renders terminal output in the right pane with a colored
// header line indicating session state (green=running, gray=ended, red=crashed).
type AgentTermView struct {
	ticketID string
	width    int
	height   int
	lines    []string
	running  bool
	crashed  bool
}

// NewAgentTermView creates a new AgentTermView for the given ticket ID.
func NewAgentTermView(ticketID string) *AgentTermView {
	return &AgentTermView{
		ticketID: ticketID,
		width:    80,
		height:   24,
		running:  true,
	}
}

// SetSize updates the dimensions. Header takes 1 row; content area is w × h-1.
func (v *AgentTermView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

// SetLines updates the rendered lines from the emulator broadcast.
func (v *AgentTermView) SetLines(lines []string) {
	v.lines = lines
}

// SetState updates the running/crashed flags that drive header color.
func (v *AgentTermView) SetState(running, crashed bool) {
	v.running = running
	v.crashed = crashed
}

// View renders the header and terminal content.
func (v *AgentTermView) View() string {
	innerH := v.height - 1 // header takes 1 row
	if innerH < 0 {
		innerH = 0
	}

	headerColor := ansiGray
	switch {
	case v.crashed:
		headerColor = ansiRed
	case v.running:
		headerColor = ansiGreen
	}

	var stateSuffix string
	if !v.running {
		if v.crashed {
			stateSuffix = " [crashed]"
		} else {
			stateSuffix = " [session ended]"
		}
	}

	headerText := fmt.Sprintf("agent: %s%s", v.ticketID, stateSuffix)

	rows := make([]string, 0, 1+innerH)
	rows = append(rows, headerColor+padOrTruncate(headerText, v.width)+ansiReset)
	for i := 0; i < innerH; i++ {
		var content string
		if i < len(v.lines) {
			content = v.lines[i]
		}
		rows = append(rows, padOrTruncate(content, v.width))
	}
	return strings.Join(rows, "\n")
}

func padOrTruncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) > width {
		return string(runes[:width])
	}
	return string(runes) + strings.Repeat(" ", width-len(runes))
}
