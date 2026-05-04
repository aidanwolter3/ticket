package views

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type reviewItem struct {
	stackID string // empty for standalone
	tickets []*model.Ticket
}

type ReviewQueueView struct {
	store  *store.Store
	items  []reviewItem
	cursor int
	width  int
	height int
	err    error
}

func NewReviewQueueView(s *store.Store) *ReviewQueueView {
	v := &ReviewQueueView{store: s}
	v.load()
	return v
}

func (v *ReviewQueueView) load() {
	q, err := v.store.ReviewQueue()
	if err != nil {
		v.err = err
		return
	}
	v.err = nil
	v.items = nil

	// Stacks first (sorted by stack ID for determinism)
	var stackIDs []string
	for id := range q.Stacks {
		stackIDs = append(stackIDs, id)
	}
	sort.Strings(stackIDs)
	for _, id := range stackIDs {
		v.items = append(v.items, reviewItem{stackID: id, tickets: q.Stacks[id]})
	}

	// Standalone
	for _, t := range q.Standalone {
		v.items = append(v.items, reviewItem{tickets: []*model.Ticket{t}})
	}

	if v.cursor >= len(v.items) {
		v.cursor = max(0, len(v.items)-1)
	}
}

func (v *ReviewQueueView) Refresh() { v.load() }

func (v *ReviewQueueView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

func (v *ReviewQueueView) SelectedItem() *reviewItem {
	if len(v.items) == 0 || v.cursor >= len(v.items) {
		return nil
	}
	return &v.items[v.cursor]
}

// FirstTicket returns the first ticket of the selected item.
func (v *ReviewQueueView) FirstTicketID() string {
	item := v.SelectedItem()
	if item == nil || len(item.tickets) == 0 {
		return ""
	}
	return item.tickets[0].ID
}

// StackTicketIDs returns all ticket IDs in the selected item (for stack walk).
func (v *ReviewQueueView) StackTicketIDs() []string {
	item := v.SelectedItem()
	if item == nil {
		return nil
	}
	var ids []string
	for _, t := range item.tickets {
		ids = append(ids, t.ID)
	}
	return ids
}

func (v *ReviewQueueView) Init() tea.Cmd { return nil }

func (v *ReviewQueueView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if v.cursor > 0 {
				v.cursor--
			}
		case "down", "j":
			if v.cursor < len(v.items)-1 {
				v.cursor++
			}
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	}
	return v, nil
}

func (v *ReviewQueueView) View() string {
	var sb strings.Builder

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Review Queue") + "\n\n")

	if v.err != nil {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Error: "+v.err.Error()) + "\n")
		return sb.String()
	}

	if len(v.items) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No tickets pending review.") + "\n")
		return sb.String()
	}

	hasStacks := false
	hasStandalone := false
	for _, item := range v.items {
		if item.stackID != "" {
			hasStacks = true
		} else {
			hasStandalone = true
		}
	}

	if hasStacks {
		sb.WriteString(lipgloss.NewStyle().Underline(true).Render("Stacks ready for review") + "\n\n")
	}

	for i, item := range v.items {
		if item.stackID == "" {
			continue
		}
		active, ready := threadCounts(item.tickets)
		summary := fmt.Sprintf("%d tickets · %d active threads · %d ready",
			len(item.tickets), active, ready)

		header := fmt.Sprintf("%s  %s",
			lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true).Render(item.stackID),
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(summary))

		if i == v.cursor {
			header = lipgloss.NewStyle().Reverse(true).Render(header)
		}
		sb.WriteString(header + "\n")

		perTicket := "  "
		for _, t := range item.tickets {
			perTicket += components.TicketStatusIcon(t.Status) + " " +
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(t.ID) + "  "
		}
		sb.WriteString(perTicket + "\n\n")
	}

	if hasStandalone {
		sb.WriteString(lipgloss.NewStyle().Underline(true).Render("Standalone tickets") + "\n\n")
	}

	for i, item := range v.items {
		if item.stackID != "" {
			continue
		}
		t := item.tickets[0]
		active, _ := threadCountsForOne(t)
		line := fmt.Sprintf("%s %s  %s",
			components.TicketStatusIcon(t.Status),
			t.Title,
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
				fmt.Sprintf("%d active threads", active)))

		if i == v.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"↑↓ navigate · enter open · r review stack · q quit"))

	return sb.String()
}

func threadCounts(tickets []*model.Ticket) (active, ready int) {
	for _, t := range tickets {
		for _, th := range t.Threads {
			switch th.Status {
			case model.ThreadActive:
				active++
			case model.ThreadReady:
				ready++
			}
		}
	}
	return
}

func threadCountsForOne(t *model.Ticket) (active, ready int) {
	return threadCounts([]*model.Ticket{t})
}
