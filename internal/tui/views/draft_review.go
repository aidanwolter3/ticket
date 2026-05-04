package views

import (
	"fmt"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	comp "github.com/aidanwolter/ticket/internal/tui/components"
)

type draftRow struct {
	ticket     *model.Ticket
	isHeader   bool
	plan       *model.Ticket // non-nil for plan-group headers; nil for standalone header
	planDrafts int
	planTotal  int
}

type DraftReviewView struct {
	store  *store.Store
	rows   []draftRow
	cursor int
	width  int
	height int
	err    error
}

func NewDraftReviewView(s *store.Store) *DraftReviewView {
	v := &DraftReviewView{store: s}
	v.load()
	return v
}

func (v *DraftReviewView) load() {
	q, err := v.store.DraftQueue()
	if err != nil {
		v.err = err
		return
	}
	v.err = nil
	v.rows = nil

	for _, dp := range q.Plans {
		draftCount := 0
		for _, c := range dp.Children {
			if c.Status == model.StatusDraft {
				draftCount++
			}
		}
		v.rows = append(v.rows, draftRow{
			isHeader:   true,
			plan:       dp.Plan,
			planDrafts: draftCount,
			planTotal:  len(dp.Children),
		})
		for _, c := range dp.Children {
			if c.Status == model.StatusDraft {
				v.rows = append(v.rows, draftRow{ticket: c})
			}
		}
	}

	if len(q.Standalone) > 0 {
		v.rows = append(v.rows, draftRow{
			isHeader:   true,
			plan:       nil,
			planDrafts: len(q.Standalone),
			planTotal:  len(q.Standalone),
		})
		for _, t := range q.Standalone {
			v.rows = append(v.rows, draftRow{ticket: t})
		}
	}

	v.clampCursor()
}

func (v *DraftReviewView) clampCursor() {
	n := len(v.rows)
	if n == 0 {
		v.cursor = 0
		return
	}
	if v.cursor >= n {
		v.cursor = n - 1
	}
	// Advance forward past headers
	for v.cursor < n && v.rows[v.cursor].isHeader {
		v.cursor++
	}
	if v.cursor >= n {
		// Try backward
		v.cursor = n - 1
		for v.cursor >= 0 && v.rows[v.cursor].isHeader {
			v.cursor--
		}
		if v.cursor < 0 {
			v.cursor = 0
		}
	}
}

func (v *DraftReviewView) moveCursor(delta int) {
	n := len(v.rows)
	if n == 0 {
		return
	}
	next := v.cursor + delta
	for next >= 0 && next < n {
		if !v.rows[next].isHeader {
			v.cursor = next
			return
		}
		next += delta
	}
}

func (v *DraftReviewView) Refresh()                    { v.load() }
func (v *DraftReviewView) SetSize(w, h int)            { v.width = w; v.height = h }
func (v *DraftReviewView) Init() tea.Cmd               { return nil }

func (v *DraftReviewView) SelectedTicket() *model.Ticket {
	if v.cursor >= len(v.rows) || v.rows[v.cursor].isHeader {
		return nil
	}
	return v.rows[v.cursor].ticket
}

func (v *DraftReviewView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			v.moveCursor(-1)
		case "down", "j":
			v.moveCursor(1)
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	}
	return v, nil
}

func (v *DraftReviewView) View() string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Draft Review") + "\n\n")

	if v.err != nil {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Error: "+v.err.Error()) + "\n")
		return sb.String()
	}

	hasDrafts := false
	for _, r := range v.rows {
		if !r.isHeader {
			hasDrafts = true
			break
		}
	}
	if !hasDrafts {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No draft tickets pending review.") + "\n")
		return sb.String()
	}

	visible := v.height - 6
	if visible < 1 {
		visible = len(v.rows)
	}
	start := 0
	if v.cursor >= visible {
		start = v.cursor - visible + 1
	}

	for i := start; i < len(v.rows) && i < start+visible; i++ {
		row := v.rows[i]
		if row.isHeader {
			if row.plan != nil {
				approved := row.planTotal - row.planDrafts
				title := row.plan.Title
				if len(title) > 40 {
					title = title[:40] + "…"
				}
				header := fmt.Sprintf("%s  %s  %s",
					lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true).Render(row.plan.ID),
					lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(title),
					comp.ProgressBar(approved, row.planTotal, 16),
				)
				sb.WriteString(header + "\n")
			} else {
				sb.WriteString(lipgloss.NewStyle().Underline(true).Render("Standalone") + "\n")
			}
			continue
		}

		t := row.ticket
		icon := components.TicketStatusIcon(t.Status)
		title := t.Title
		if len(title) > 60 {
			title = title[:60] + "…"
		}
		line := fmt.Sprintf("  %s %s  %s",
			icon,
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(t.ID),
			title,
		)
		if i == v.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"↑↓ navigate · enter open · a approve → ready · d delete · q quit"))

	return sb.String()
}
