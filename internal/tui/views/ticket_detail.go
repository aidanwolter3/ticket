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

	comp "github.com/aidanwolter/ticket/internal/tui/components"
)

type TicketDetailView struct {
	store    *store.Store
	ticket   *model.Ticket
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

	// Populate per-task threads so the detail view can show counts.
	for i := range v.ticket.Tasks {
		threads, err := v.store.GetThreadsForTask(v.ticket.Tasks[i].ID)
		if err != nil {
			return err
		}
		v.ticket.Tasks[i].Threads = nil
		for _, th := range threads {
			v.ticket.Tasks[i].Threads = append(v.ticket.Tasks[i].Threads, *th)
		}
	}

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
	if v.width == w && v.height == h {
		return
	}
	v.width = w
	v.height = h
	v.vp = viewport.New(w, h-2)
	v.vp.SetContent(v.renderContent())
}

func (v *TicketDetailView) ScrollUp(n int)   { v.vp.LineUp(n) }
func (v *TicketDetailView) ScrollDown(n int) { v.vp.LineDown(n) }

func (v *TicketDetailView) Init() tea.Cmd { return nil }

func (v *TicketDetailView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		v.SetSize(ws.Width, ws.Height)
		return v, nil
	}
	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	return v, cmd
}

func (v *TicketDetailView) View() string {
	v.vp.SetContent(v.renderContent())
	return v.vp.View() + "\n" +
		lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
			"e edit · t threads · n note · s status · [ ] scroll · esc back")
}

func (v *TicketDetailView) renderContent() string {
	if v.ticket == nil {
		return "No ticket loaded."
	}
	t := v.ticket
	var sb strings.Builder

	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(
		fmt.Sprintf("%s  %s", t.ID, t.Title)) + "\n")

	sb.WriteString(fmt.Sprintf("  Status: %s  %s\n",
		components.TicketStatusIcon(t.Status)+" "+string(t.Status),
		lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("Updated: "+t.Updated.Format(time.RFC1123)),
	))

	if t.FeatureBranch != "" {
		sb.WriteString(fmt.Sprintf("  Branch: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render(t.FeatureBranch)))
	}
	if t.WorktreePath != "" {
		sb.WriteString(fmt.Sprintf("  Worktree: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render(t.WorktreePath)))
	}
	sb.WriteString("\n")

	wrapWidth := v.width - 2
	if t.Description != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Description") + "\n")
		sb.WriteString(indent(wrapText(t.Description, wrapWidth), "  ") + "\n\n")
	}

	if len(v.blockers) > 0 {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Blocked By") + "\n")
		for _, b := range v.blockers {
			sb.WriteString(fmt.Sprintf("  %s %s %s\n",
				components.TicketStatusIcon(b.Status), b.ID, b.Title))
		}
		sb.WriteString("\n")
	}

	// Tasks section
	completed := 0
	for _, task := range t.Tasks {
		if task.CompletedAt != nil {
			completed++
		}
	}
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Tasks (%d/%d)", completed, len(t.Tasks))) + "\n")
	if len(t.Tasks) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  no tasks") + "\n")
	} else {
		sb.WriteString("  " + comp.ProgressBar(completed, len(t.Tasks), 20) + "\n")
		for _, task := range t.Tasks {
			icon := "○"
			col := lipgloss.Color("8")
			if task.CompletedAt != nil {
				icon = "●"
				col = lipgloss.Color("2")
			}
			taskLine := fmt.Sprintf("  %s %s. %s",
				lipgloss.NewStyle().Foreground(col).Render(icon),
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(fmt.Sprintf("%d", task.Position)),
				task.Title,
			)
			if task.CommitHash != "" {
				taskLine += " " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(task.CommitHash[:7])
			}
			if len(task.Threads) > 0 {
				active, ready := 0, 0
				for _, th := range task.Threads {
					switch th.Status {
					case model.ThreadActive:
						active++
					case model.ThreadReady:
						ready++
					}
				}
				var parts []string
				if active > 0 {
					parts = append(parts, fmt.Sprintf("%d active", active))
				}
				if ready > 0 {
					parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(fmt.Sprintf("%d ready", ready)))
				}
				if len(parts) > 0 {
					taskLine += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("("+strings.Join(parts, " · ")+")")
				}
			}
			sb.WriteString(taskLine + "\n")
			if task.Description != "" {
				sb.WriteString(indent(wrapText(task.Description, wrapWidth-4), "      ") + "\n")
			}
			if task.VerifiableResult != "" {
				sb.WriteString("      " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("✓ "+task.VerifiableResult) + "\n")
			}
		}
	}
	sb.WriteString("\n")

	// Thread summary (aggregated across all tasks)
	active, ready, resolved := 0, 0, 0
	for _, task := range t.Tasks {
		for _, th := range task.Threads {
			switch th.Status {
			case model.ThreadActive:
				active++
			case model.ThreadReady:
				ready++
			case model.ThreadResolved:
				resolved++
			}
		}
	}
	total := active + ready + resolved
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Threads (%d)", total)) + "\n")
	if total == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  no threads") + "\n")
	} else {
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
