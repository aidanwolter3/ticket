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
	itemTask         threadItemKind = iota
	itemThread                      // real thread
	itemDraftThread                 // draft thread not yet submitted
	itemDraftMessage                // draft message (in draft thread or reply to real thread)
)

type threadItem struct {
	kind         threadItemKind
	task         model.Task
	thread       *model.Thread      // set when kind == itemThread
	draftThread  *model.DraftThread // set when kind == itemDraftThread
	draftMsg     model.DraftMessage // set when kind == itemDraftMessage
	stagedAction string             // for real threads: "resolve"/"reopen"/""
	draftReplies []model.DraftMessage
}

type ThreadsView struct {
	store      *store.Store
	ticketID   string
	items      []threadItem
	cursor     int
	expanded   map[string]bool
	draftState *model.DraftState
	width      int
	height     int
	err        error
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
	ds, err := v.store.GetDraftState(v.ticketID)
	if err != nil {
		return err
	}
	v.draftState = ds

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
			item := threadItem{
				kind:         itemThread,
				task:         task,
				thread:       th,
				stagedAction: ds.ActionFor(th.ID),
				draftReplies: ds.RepliesFor(th.ID),
			}
			v.items = append(v.items, item)
			// When expanded, draft replies become selectable cursor items.
			if v.expanded[th.ID] {
				for _, dr := range item.draftReplies {
					v.items = append(v.items, threadItem{
						kind:     itemDraftMessage,
						task:     task,
						draftMsg: dr,
					})
				}
			}
		}

		// Draft threads for this task.
		for i := range ds.NewThreads {
			dt := &ds.NewThreads[i]
			if dt.TaskID != task.ID {
				continue
			}
			v.items = append(v.items, threadItem{
				kind:        itemDraftThread,
				task:        task,
				draftThread: dt,
			})
			// When expanded, draft thread messages become selectable cursor items.
			if v.expanded[dt.ID] {
				for _, msg := range dt.Messages {
					v.items = append(v.items, threadItem{
						kind:     itemDraftMessage,
						task:     task,
						draftMsg: msg,
					})
				}
			}
		}
	}
	if v.cursor >= len(v.items) {
		v.cursor = max(0, len(v.items)-1)
	}
	return nil
}

func (v *ThreadsView) Reload() error { return v.load() }
func (v *ThreadsView) TicketID() string { return v.ticketID }

// SelectedThread returns the highlighted real thread, or nil.
func (v *ThreadsView) SelectedThread() *model.Thread {
	if v.cursor < len(v.items) && v.items[v.cursor].kind == itemThread {
		return v.items[v.cursor].thread
	}
	return nil
}

// SelectedDraftMessage returns the highlighted draft message, or nil.
func (v *ThreadsView) SelectedDraftMessage() *model.DraftMessage {
	if v.cursor < len(v.items) && v.items[v.cursor].kind == itemDraftMessage {
		m := v.items[v.cursor].draftMsg
		return &m
	}
	return nil
}

// SelectedTaskID returns the task ID for the current cursor position.
func (v *ThreadsView) SelectedTaskID() string {
	if v.cursor < len(v.items) {
		return v.items[v.cursor].task.ID
	}
	return ""
}

// HasDraft reports whether there is any staged draft content.
func (v *ThreadsView) HasDraft() bool {
	return v.draftState != nil && !v.draftState.IsEmpty()
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
			if v.cursor < len(v.items) {
				switch v.items[v.cursor].kind {
				case itemThread:
					id := v.items[v.cursor].thread.ID
					v.expanded[id] = !v.expanded[id]
					v.load()
				case itemDraftThread:
					id := v.items[v.cursor].draftThread.ID
					v.expanded[id] = !v.expanded[id]
					v.load()
				}
			}
		case "x":
			if v.cursor < len(v.items) && v.items[v.cursor].kind == itemThread {
				item := v.items[v.cursor]
				th := item.thread
				switch th.Status {
				case model.ThreadOpen, model.ThreadNeedsAttention:
					if item.stagedAction == model.DraftActionResolve {
						v.err = v.store.ClearDraftAction(th.ID)
					} else {
						v.err = v.store.SetDraftAction(th.ID, v.ticketID, model.DraftActionResolve)
					}
				case model.ThreadResolved:
					if item.stagedAction == model.DraftActionReopen {
						v.err = v.store.ClearDraftAction(th.ID)
					} else {
						v.err = v.store.SetDraftAction(th.ID, v.ticketID, model.DraftActionReopen)
					}
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

			hasContent := i+1 < len(v.items) && v.items[i+1].kind != itemTask
			if !hasContent {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  (no threads — press n to start one)") + "\n")
			}

		case itemThread:
			th := item.thread
			icon := v.threadIcon(th.Status, item.stagedAction)
			summary := th.Summary()
			totalMsgs := len(th.Messages) + len(item.draftReplies)
			msgCount := fmt.Sprintf("(%d msg)", totalMsgs)

			suffix := ""
			switch item.stagedAction {
			case model.DraftActionResolve:
				suffix = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[→resolved]")
			case model.DraftActionReopen:
				suffix = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[→open]")
			}

			line := fmt.Sprintf("  %s %s%s %s",
				icon, summary, suffix,
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
			}

		case itemDraftThread:
			dt := item.draftThread
			draftIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("◌")
			summary := "(empty)"
			if len(dt.Messages) > 0 {
				s := dt.Messages[0].Text
				if len(s) > 60 {
					s = s[:60] + "…"
				}
				summary = s
			}
			msgCount := fmt.Sprintf("(%d msg)", len(dt.Messages))

			line := fmt.Sprintf("  %s %s %s %s",
				draftIcon, summary,
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(msgCount),
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[draft]"),
			)
			if i == v.cursor {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			} else {
				line = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(line)
			}
			sb.WriteString(line + "\n")

		case itemDraftMessage:
			dm := item.draftMsg
			author := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(dm.Author + " [draft]")
			msgWrap := v.width - 8 - len([]rune(dm.Author)) - 8
			text := wrapText(dm.Text, msgWrap)
			line := fmt.Sprintf("    %s: %s", author, text)
			if i == v.cursor {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			}
			sb.WriteString(line + "\n")
		}
	}

	sb.WriteString("\n")
	isDraftMsg := v.cursor < len(v.items) && v.items[v.cursor].kind == itemDraftMessage
	var hint string
	if isDraftMsg {
		hint = "[e] edit · [D] delete · [esc] back"
	} else {
		hintExtra := ""
		if v.HasDraft() {
			hintExtra = " · [ctrl+s] submit review"
		}
		hint = "[↑↓] navigate · [enter] expand · [r] reply · [x] toggle resolve · [n] new thread · [esc] back" + hintExtra
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(hint))

	return sb.String()
}

// threadIcon returns the display icon for a thread, taking staged action into account.
func (v *ThreadsView) threadIcon(status model.ThreadStatus, staged string) string {
	if staged == model.DraftActionResolve {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("✓")
	}
	if staged == model.DraftActionReopen {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("●")
	}
	return components.ThreadStatusIcon(status)
}
