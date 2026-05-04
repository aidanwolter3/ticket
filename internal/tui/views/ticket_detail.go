package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/components"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type TicketDetailView struct {
	store    *store.Store
	ticket   *model.Ticket
	threads  []*model.Thread
	notes    []*model.Note
	blockers []*model.Ticket
	vp       viewport.Model
	width    int
	height   int
	err      error
}

func NewTicketDetailView(s *store.Store, ticketID string) (*TicketDetailView, error) {
	v := &TicketDetailView{store: s}
	if err := v.loadTicket(ticketID); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *TicketDetailView) loadTicket(id string) error {
	t, err := v.store.GetTicket(id)
	if err != nil {
		return err
	}
	v.ticket = t

	threads, err := v.store.GetThreadsForTicket(id)
	if err != nil {
		return err
	}
	v.threads = threads

	notes, err := v.store.GetNotesForTicket(id)
	if err != nil {
		return err
	}
	v.notes = notes

	v.blockers = nil
	for _, blockerID := range t.BlockedBy {
		blocker, err := v.store.GetTicket(blockerID)
		if err == nil {
			v.blockers = append(v.blockers, blocker)
		}
	}
	return nil
}

func (v *TicketDetailView) Reload() error {
	if v.ticket == nil {
		return nil
	}
	return v.loadTicket(v.ticket.ID)
}

func (v *TicketDetailView) Ticket() *model.Ticket {
	return v.ticket
}

func (v *TicketDetailView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.vp = viewport.New(w, h-2)
	v.vp.SetContent(v.renderContent())
}

func (v *TicketDetailView) Init() tea.Cmd { return nil }

func (v *TicketDetailView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)
	default:
		var cmd tea.Cmd
		v.vp, cmd = v.vp.Update(msg)
		return v, cmd
	}
	return v, nil
}

func (v *TicketDetailView) View() string {
	v.vp.SetContent(v.renderContent())
	return v.vp.View() + "\n" +
		lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
			"e edit · t threads · n note · b blockers · s status · esc back")
}

func (v *TicketDetailView) renderContent() string {
	if v.ticket == nil {
		return "No ticket loaded."
	}
	t := v.ticket
	var sb strings.Builder

	typeColor := lipgloss.Color("15")
	if t.IsPlan() {
		typeColor = lipgloss.Color("6")
	}

	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(typeColor).Render(
		fmt.Sprintf("%s  %s", t.ID, t.Title)) + "\n")

	sb.WriteString(fmt.Sprintf("  Type: %s  Status: %s  %s\n",
		string(t.Type),
		components.TicketStatusIcon(t.Status)+" "+string(t.Status),
		lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("Updated: "+t.Updated.Format(time.RFC1123)),
	))

	if t.FeatureBranch != "" {
		sb.WriteString(fmt.Sprintf("  Branch: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render(t.FeatureBranch)))
	}
	if t.StackID != "" {
		sb.WriteString(fmt.Sprintf("  Stack: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render(t.StackID)))
	}
	if t.CommitHash != "" {
		sb.WriteString(fmt.Sprintf("  Commit: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(t.CommitHash)))
	}
	sb.WriteString("\n")

	wrapWidth := v.width - 2
	if t.Description != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Description") + "\n")
		sb.WriteString(indent(wrapText(t.Description, wrapWidth), "  ") + "\n\n")
	}

	if t.VerifiableResult != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Verifiable Result") + "\n")
		sb.WriteString(indent(wrapText(t.VerifiableResult, wrapWidth), "  ") + "\n\n")
	}

	if len(v.blockers) > 0 {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Blocked By") + "\n")
		for _, b := range v.blockers {
			sb.WriteString(fmt.Sprintf("  %s %s %s\n",
				components.TicketStatusIcon(b.Status), b.ID, b.Title))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Threads (%d)", len(v.threads))) + "\n")
	if len(v.threads) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  no threads") + "\n")
	} else {
		active, ready, resolved := 0, 0, 0
		for _, th := range v.threads {
			switch th.Status {
			case model.ThreadActive:
				active++
			case model.ThreadReady:
				ready++
			case model.ThreadResolved:
				resolved++
			}
		}
		sb.WriteString(fmt.Sprintf("  %s active  %s ready  %s resolved\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render(fmt.Sprintf("%d", active)),
			lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(fmt.Sprintf("%d", ready)),
			lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(fmt.Sprintf("%d", resolved)),
		))
	}
	sb.WriteString("\n")

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Notes (%d)", len(v.notes))) + "\n")
	if len(v.notes) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  no notes") + "\n")
	} else {
		for _, n := range v.notes {
			authorStr := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(n.Author)
			noteWrap := v.width - 4 - len([]rune(n.Author))
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", authorStr, wrapText(n.Text, noteWrap)))
		}
	}

	return sb.String()
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// wrapText hard-wraps s at width columns, preserving existing newlines.
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, wrapLine(line, width))
	}
	return strings.Join(out, "\n")
}

func wrapLine(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		if len([]rune(line))+1+len([]rune(word)) <= width {
			line += " " + word
		} else {
			lines = append(lines, line)
			line = word
		}
	}
	return strings.Join(append(lines, line), "\n")
}
