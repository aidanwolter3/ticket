package views

import (
	"fmt"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ThreadsView struct {
	store    *store.Store
	ticketID string
	threads  []*model.Thread
	cursor   int
	expanded map[string]bool
	width    int
	height   int
	err      error
}

func NewThreadsView(s *store.Store, ticketID string) (*ThreadsView, error) {
	v := &ThreadsView{
		store:    s,
		ticketID: ticketID,
		expanded: make(map[string]bool),
	}
	return v, v.load()
}

func (v *ThreadsView) load() error {
	threads, err := v.store.GetThreadsForTicket(v.ticketID)
	if err != nil {
		return err
	}
	v.threads = threads
	if v.cursor >= len(v.threads) {
		v.cursor = max(0, len(v.threads)-1)
	}
	return nil
}

func (v *ThreadsView) Reload() error            { return v.load() }
func (v *ThreadsView) Threads() []*model.Thread { return v.threads }
func (v *ThreadsView) Cursor() int              { return v.cursor }
func (v *ThreadsView) TicketID() string         { return v.ticketID }

func (v *ThreadsView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

func (v *ThreadsView) Init() tea.Cmd { return nil }

func (v *ThreadsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if v.cursor > 0 {
				v.cursor--
			}
		case "down", "j":
			if v.cursor < len(v.threads)-1 {
				v.cursor++
			}
		case "enter":
			if v.cursor < len(v.threads) {
				id := v.threads[v.cursor].ID
				v.expanded[id] = !v.expanded[id]
			}
		case "right":
			if v.cursor < len(v.threads) {
				th := v.threads[v.cursor]
				var to model.ThreadStatus
				switch th.Status {
				case model.ThreadActive:
					to = model.ThreadReady
				case model.ThreadReady:
					to = model.ThreadActive
				default:
					return v, nil
				}
				v.err = v.store.TransitionThread(th.ID, to, "human")
				v.load()
			}
		case "x":
			if v.cursor < len(v.threads) {
				th := v.threads[v.cursor]
				v.err = v.store.TransitionThread(th.ID, model.ThreadResolved, "human")
				v.load()
			}
		case "left":
			if v.cursor < len(v.threads) {
				th := v.threads[v.cursor]
				if th.Status == model.ThreadResolved {
					v.err = v.store.TransitionThread(th.ID, model.ThreadActive, "human")
					v.load()
				}
			}
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	}
	return v, nil
}

func (v *ThreadsView) View() string {
	var sb strings.Builder

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Threads for %s", v.ticketID)) + "\n\n")

	if v.err != nil {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Error: "+v.err.Error()) + "\n")
	}

	if len(v.threads) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No threads. Press 'n' to create one.") + "\n")
	}

	for i, th := range v.threads {
		icon := components.ThreadStatusIcon(th.Status)
		summary := th.Summary()
		msgCount := fmt.Sprintf("(%d msg)", len(th.Messages))

		line := fmt.Sprintf("%s %s %s",
			icon, summary,
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(msgCount))

		if i == v.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		sb.WriteString(line + "\n")

		if v.expanded[th.ID] {
			for _, msg := range th.Messages {
				author := lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render(msg.Author)
				sb.WriteString(fmt.Sprintf("    %s: %s\n", author, msg.Text))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"↑↓ navigate · enter expand · r reply · → toggle ready · x resolve · ← reopen · n new · esc back"))

	return sb.String()
}
