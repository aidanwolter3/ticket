package views

import (
	"fmt"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/components"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	comp "github.com/aidanwolter/ticket/internal/tui/components"
)

type PlanDetailView struct {
	store    *store.Store
	ticket   *model.Ticket
	children []*model.Ticket
	threads  []*model.Thread
	notes    []*model.Note
	vp       viewport.Model
	width    int
	height   int
	err      error
}

func NewPlanDetailView(s *store.Store, ticketID string) (*PlanDetailView, error) {
	v := &PlanDetailView{store: s}
	return v, v.load(ticketID)
}

func (v *PlanDetailView) load(id string) error {
	t, err := v.store.GetTicket(id)
	if err != nil {
		return err
	}
	v.ticket = t
	v.children = nil
	for _, childID := range t.BlockedBy {
		child, err := v.store.GetTicket(childID)
		if err == nil {
			v.children = append(v.children, child)
		}
	}
	v.threads, _ = v.store.GetThreadsForTicket(id)
	v.notes, _ = v.store.GetNotesForTicket(id)
	return nil
}

func (v *PlanDetailView) Reload() error {
	if v.ticket == nil {
		return nil
	}
	return v.load(v.ticket.ID)
}

func (v *PlanDetailView) Ticket() *model.Ticket { return v.ticket }

func (v *PlanDetailView) SetSize(w, h int) {
	if v.width == w && v.height == h {
		return
	}
	v.width = w
	v.height = h
	v.vp = viewport.New(w, h-2)
	v.vp.SetContent(v.renderContent())
}

func (v *PlanDetailView) ScrollUp(n int)   { v.vp.LineUp(n) }
func (v *PlanDetailView) ScrollDown(n int) { v.vp.LineDown(n) }

func (v *PlanDetailView) Init() tea.Cmd { return nil }

func (v *PlanDetailView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		v.SetSize(ws.Width, ws.Height)
		return v, nil
	}
	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	return v, cmd
}

func (v *PlanDetailView) View() string {
	v.vp.SetContent(v.renderContent())
	return v.vp.View() + "\n" +
		lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
			"e edit · t threads · n note · s status · [ ] scroll · esc back")
}

func (v *PlanDetailView) renderContent() string {
	if v.ticket == nil {
		return "No plan loaded."
	}
	t := v.ticket
	var sb strings.Builder

	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(
		fmt.Sprintf("%s  %s  [plan]", t.ID, t.Title)) + "\n")
	sb.WriteString(fmt.Sprintf("  Status: %s\n\n",
		components.TicketStatusIcon(t.Status)+" "+string(t.Status)))

	if t.Description != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Description") + "\n")
		sb.WriteString(indent(wrapText(t.Description, v.width-2), "  ") + "\n\n")
	}

	completed := 0
	for _, c := range v.children {
		if c.Status == model.StatusCompleted {
			completed++
		}
	}
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Progress") + "\n")
	sb.WriteString("  " + comp.ProgressBar(completed, len(v.children), 20) + "\n\n")

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Children (%d)", len(v.children))) + "\n")
	if len(v.children) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  no children") + "\n")
	} else {
		for _, c := range v.children {
			sb.WriteString(fmt.Sprintf("  %s %s  %s\n",
				components.TicketStatusIcon(c.Status),
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(c.ID),
				c.Title))
		}
	}
	sb.WriteString("\n")

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Threads (%d)", len(v.threads))) + "\n\n")

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Notes (%d)", len(v.notes))) + "\n")
	for _, n := range v.notes {
		sb.WriteString(fmt.Sprintf("  [%s] %s\n", n.Author, n.Text))
	}

	return sb.String()
}
