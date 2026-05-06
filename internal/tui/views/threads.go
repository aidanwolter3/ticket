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

type threadItemKind int

const (
	itemTask   threadItemKind = iota
	itemThread
)

type threadItem struct {
	kind   threadItemKind
	task   model.Task
	thread *model.Thread // nil when kind == itemTask
}

type ThreadsView struct {
	store    *store.Store
	ticketID string
	items    []threadItem
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
	tasks, err := v.store.GetTasksForTicket(v.ticketID)
	if err != nil {
		return err
	}
	v.items = nil
	for _, task := range tasks {
		v.items = append(v.items, threadItem{kind: itemTask, task: task})
		threads, err := v.store.GetThreadsForTask(task.ID)
		if err != nil {
			return err
		}
		for _, th := range threads {
			v.items = append(v.items, threadItem{kind: itemThread, task: task, thread: th})
		}
	}
	if v.cursor >= len(v.items) {
		v.cursor = max(0, len(v.items)-1)
	}
	return nil
}

func (v *ThreadsView) Reload() error { return v.load() }
func (v *ThreadsView) TicketID() string { return v.ticketID }

// SelectedThread returns the highlighted thread, or nil if on a task row.
func (v *ThreadsView) SelectedThread() *model.Thread {
	if v.cursor < len(v.items) && v.items[v.cursor].kind == itemThread {
		return v.items[v.cursor].thread
	}
	return nil
}

// SelectedTaskID returns the task ID for the current cursor position
// (works whether the cursor is on a task row or a thread row).
func (v *ThreadsView) SelectedTaskID() string {
	if v.cursor < len(v.items) {
		return v.items[v.cursor].task.ID
	}
	return ""
}

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
			if v.cursor < len(v.items)-1 {
				v.cursor++
			}
		case "enter":
			if v.cursor < len(v.items) && v.items[v.cursor].kind == itemThread {
				id := v.items[v.cursor].thread.ID
				v.expanded[id] = !v.expanded[id]
			}
		case "right":
			if v.cursor < len(v.items) && v.items[v.cursor].kind == itemThread {
				th := v.items[v.cursor].thread
				var to model.ThreadStatus
				switch th.Status {
				case model.ThreadOpen:
					to = model.ThreadNeedsAttention
				case model.ThreadNeedsAttention:
					to = model.ThreadOpen
				default:
					return v, nil
				}
				v.err = v.store.TransitionThread(th.ID, to, "human")
				v.load()
			}
		case "x":
			if v.cursor < len(v.items) && v.items[v.cursor].kind == itemThread {
				th := v.items[v.cursor].thread
				v.err = v.store.TransitionThread(th.ID, model.ThreadResolved, "human")
				v.load()
			}
		case "left":
			if v.cursor < len(v.items) && v.items[v.cursor].kind == itemThread {
				th := v.items[v.cursor].thread
				if th.Status == model.ThreadResolved {
					v.err = v.store.TransitionThread(th.ID, model.ThreadOpen, "human")
					v.load()
				}
			}
		case "c":
			if v.cursor < len(v.items) && v.items[v.cursor].kind == itemTask {
				task := v.items[v.cursor].task
				if task.CompletedAt != nil {
					v.err = v.store.UncompleteTask(task.ID)
				} else {
					v.err = v.store.CompleteTask(task.ID)
				}
				v.load()
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

	if len(v.items) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No tasks found.") + "\n")
	}

	for i, item := range v.items {
		switch item.kind {
		case itemTask:
			if i > 0 {
				sb.WriteString("\n")
			}
			completionIcon := "○"
			completionCol := lipgloss.Color("8")
			if item.task.CompletedAt != nil {
				completionIcon = "●"
				completionCol = lipgloss.Color("2")
			}
			taskLine := fmt.Sprintf("%s %s  %d. %s",
				lipgloss.NewStyle().Foreground(completionCol).Render(completionIcon),
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(item.task.ID),
				item.task.Position,
				item.task.Title,
			)
			if i == v.cursor {
				taskLine = lipgloss.NewStyle().Reverse(true).Render(taskLine)
			} else {
				taskLine = lipgloss.NewStyle().Bold(true).Render(taskLine)
			}
			sb.WriteString(taskLine + "\n")

			// Show "(no threads)" when this task has no threads.
			hasThreads := i+1 < len(v.items) && v.items[i+1].kind == itemThread
			if !hasThreads {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  (no threads — press n to start one)") + "\n")
			}

		case itemThread:
			th := item.thread
			icon := components.ThreadStatusIcon(th.Status)
			summary := th.Summary()
			msgCount := fmt.Sprintf("(%d msg)", len(th.Messages))

			line := fmt.Sprintf("  %s %s %s",
				icon, summary,
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(msgCount))

			if i == v.cursor {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			}
			sb.WriteString(line + "\n")

			if v.expanded[th.ID] {
				for _, msg := range th.Messages {
					author := lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render(msg.Author)
					msgWrap := v.width - 8 - len([]rune(msg.Author))
					sb.WriteString(fmt.Sprintf("    %s: %s\n", author, wrapText(msg.Text, msgWrap)))
				}
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"[↑↓] navigate · [c] complete task · [enter] expand · [r] reply · [→] toggle ready · [x] resolve · [←] reopen · [n] new thread · [esc] back"))

	return sb.String()
}
