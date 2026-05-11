package views

import (
	"testing"

	"github.com/aidanwolter/ticket/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sendKey sends a key event to the ReviewPanelView and returns the updated model.
func sendKey(v *ReviewPanelView, key string) *ReviewPanelView {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	if key == "enter" {
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	} else if key == "up" {
		msg = tea.KeyMsg{Type: tea.KeyUp}
	} else if key == "down" {
		msg = tea.KeyMsg{Type: tea.KeyDown}
	}
	m, _ := v.Update(msg)
	return m.(*ReviewPanelView)
}

// newReviewPanel is a helper that creates a seeded ReviewPanelView for tests.
func newReviewPanel(t *testing.T) (*ReviewPanelView, *model.Ticket) {
	t.Helper()
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "Test Ticket", Type: model.TypeTicket, Status: model.StatusInReview}
	require.NoError(t, s.CreateTicket(ticket))
	v, err := NewReviewPanelView(s, ticket.ID)
	require.NoError(t, err)
	v.SetSize(120, 40)
	return v, ticket
}

func TestReviewPanel_LeftItems(t *testing.T) {
	t.Run("two tasks no threads not expanded", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task1 := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
		task2 := &model.Task{TicketID: ticket.ID, Title: "Task 2", Position: 2}
		require.NoError(t, s.CreateTask(task1))
		require.NoError(t, s.CreateTask(task2))

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		assert.Len(t, v.leftItems, 2)
		assert.Equal(t, leftKindTask, v.leftItems[0].kind)
		assert.Equal(t, leftKindTask, v.leftItems[1].kind)
	})

	t.Run("one task with threads collapsed shows expand marker and counts", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
		require.NoError(t, s.CreateTask(task))

		// One open thread with a message.
		th1, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th1.ID, "human:aidan", "needs work")
		require.NoError(t, err)

		// One resolved thread with a message.
		th2, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th2.ID, "agent:claude", "done")
		require.NoError(t, err)
		_, err = s.DB().Exec(`UPDATE comment_threads SET status='resolved' WHERE id=?`, th2.ID)
		require.NoError(t, err)

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Collapsed: only the task item is visible.
		assert.Len(t, v.leftItems, 1)
		assert.Equal(t, leftKindTask, v.leftItems[0].kind)

		// View output should show the expand marker and counts.
		out := v.View()
		assert.Contains(t, out, "▶")
		assert.Contains(t, out, "1●")
		assert.Contains(t, out, "1✓")
	})

	t.Run("task expanded shows thread items", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
		require.NoError(t, s.CreateTask(task))

		th1, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th1.ID, "human:aidan", "comment 1")
		require.NoError(t, err)

		th2, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th2.ID, "agent:claude", "comment 2")
		require.NoError(t, err)

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Expand with enter.
		v = sendKey(v, "enter")

		assert.Len(t, v.leftItems, 3) // 1 task + 2 threads
		assert.Equal(t, leftKindTask, v.leftItems[0].kind)
		assert.Equal(t, leftKindThread, v.leftItems[1].kind)
		assert.Equal(t, leftKindThread, v.leftItems[2].kind)

		// Second enter collapses again.
		v = sendKey(v, "enter")
		assert.Len(t, v.leftItems, 1)
	})

	t.Run("thread expanded shows message items then collapses", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
		require.NoError(t, s.CreateTask(task))

		th, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th.ID, "human:aidan", "msg 1")
		require.NoError(t, err)
		_, err = s.AddMessage(th.ID, "agent:claude", "msg 2")
		require.NoError(t, err)

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Expand task.
		v = sendKey(v, "enter")
		// Move cursor to thread item.
		v = sendKey(v, "j")
		// Expand thread.
		v = sendKey(v, "enter")

		// Should have: task + thread + 2 messages.
		assert.Len(t, v.leftItems, 4)
		assert.Equal(t, leftKindMessage, v.leftItems[2].kind)
		assert.Equal(t, leftKindMessage, v.leftItems[3].kind)

		// Enter again on the thread item collapses messages.
		v = sendKey(v, "enter")
		assert.Len(t, v.leftItems, 2) // task + thread, no messages
	})

	t.Run("draft thread entry appears when task expanded", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
		require.NoError(t, s.CreateTask(task))

		dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human:aidan", "draft comment")
		require.NoError(t, err)

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Expand task to see draft thread.
		v = sendKey(v, "enter")

		kinds := make([]leftItemKind, len(v.leftItems))
		for i, item := range v.leftItems {
			kinds[i] = item.kind
		}
		assert.Contains(t, kinds, leftKindDraftThread)

		// View should show draft indicator and label.
		out := v.View()
		assert.Contains(t, stripANSI(out), "◌")
		assert.Contains(t, stripANSI(out), "[draft]")
	})

	t.Run("draft reply on real thread appears when thread expanded", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
		require.NoError(t, s.CreateTask(task))

		th, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th.ID, "human:aidan", "original message")
		require.NoError(t, err)

		// Draft reply to the real thread.
		_, err = s.AddDraftMessage(th.ID, ticket.ID, true, "human:aidan", "draft reply text")
		require.NoError(t, err)

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Expand task, move to thread, expand thread.
		v = sendKey(v, "enter")
		v = sendKey(v, "j")
		v = sendKey(v, "enter")

		kinds := make([]leftItemKind, len(v.leftItems))
		for i, item := range v.leftItems {
			kinds[i] = item.kind
		}
		assert.Contains(t, kinds, leftKindDraftMessage)
	})
}

func TestReviewPanel_Nav(t *testing.T) {
	t.Run("j moves cursor down stops at end", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		for i := 1; i <= 3; i++ {
			task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: i}
			require.NoError(t, s.CreateTask(task))
		}
		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Navigate down 10 times, should stop at len-1=2.
		for i := 0; i < 10; i++ {
			v = sendKey(v, "j")
		}
		assert.Equal(t, 2, v.leftCursor)
	})

	t.Run("k moves cursor up stops at 0", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		for i := 1; i <= 3; i++ {
			task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: i}
			require.NoError(t, s.CreateTask(task))
		}
		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Move to end then try to go past 0.
		v = sendKey(v, "j")
		v = sendKey(v, "j")
		for i := 0; i < 10; i++ {
			v = sendKey(v, "k")
		}
		assert.Equal(t, 0, v.leftCursor)
	})

	t.Run("enter on task toggles expand twice", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: 1}
		require.NoError(t, s.CreateTask(task))
		th, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th.ID, "human:aidan", "msg")
		require.NoError(t, err)

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		v = sendKey(v, "enter")
		assert.True(t, v.expandedTasks[task.ID], "task should be expanded after first enter")
		v = sendKey(v, "enter")
		assert.False(t, v.expandedTasks[task.ID], "task should be collapsed after second enter")
	})

	t.Run("enter on thread sets and clears expandedThread", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: 1}
		require.NoError(t, s.CreateTask(task))
		th, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th.ID, "human:aidan", "msg")
		require.NoError(t, err)

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Expand task, move to thread, expand thread.
		v = sendKey(v, "enter")
		v = sendKey(v, "j")
		v = sendKey(v, "enter")
		assert.Equal(t, th.ID, v.expandedThread)

		// Enter again collapses thread; cursor repositions to the thread item.
		v = sendKey(v, "enter")
		assert.Empty(t, v.expandedThread)
		// Cursor should be on the thread item.
		require.Less(t, v.leftCursor, len(v.leftItems))
		assert.Equal(t, leftKindThread, v.leftItems[v.leftCursor].kind)
	})

	t.Run("[ scroll down clamped at 0", func(t *testing.T) {
		v, _ := newReviewPanel(t)
		// When offset is already 0, '[' should keep it at 0.
		v = sendKey(v, "[")
		assert.Equal(t, 0, v.offset)
	})

	t.Run("] increments offset", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: 1}
		require.NoError(t, s.CreateTask(task))
		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		// Use small height (bodyH=5) so 7 content lines can scroll.
		v.SetSize(120, 9)

		v = sendKey(v, "]")
		assert.Equal(t, 1, v.offset)
	})

	t.Run("< clamped at hOffset 0", func(t *testing.T) {
		v, _ := newReviewPanel(t)
		v.hOffset = 0
		v = sendKey(v, "<")
		assert.Equal(t, 0, v.hOffset)
	})

	t.Run("> increments hOffset", func(t *testing.T) {
		v, _ := newReviewPanel(t)
		v = sendKey(v, ">")
		assert.Equal(t, 4, v.hOffset)
	})
}

func TestReviewPanel_Accessors(t *testing.T) {
	t.Run("SelectedTaskID returns correct task at each cursor", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task1 := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
		task2 := &model.Task{TicketID: ticket.ID, Title: "Task 2", Position: 2}
		require.NoError(t, s.CreateTask(task1))
		require.NoError(t, s.CreateTask(task2))

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		assert.Equal(t, task1.ID, v.SelectedTaskID())
		v = sendKey(v, "j")
		assert.Equal(t, task2.ID, v.SelectedTaskID())
	})

	t.Run("SelectedThread returns nil on task item", func(t *testing.T) {
		v, _ := newReviewPanel(t)
		assert.Nil(t, v.SelectedThread())
	})

	t.Run("SelectedThread returns thread when cursor on thread item", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: 1}
		require.NoError(t, s.CreateTask(task))
		th, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th.ID, "human:aidan", "msg")
		require.NoError(t, err)

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Expand task and move to thread.
		v = sendKey(v, "enter")
		v = sendKey(v, "j")

		got := v.SelectedThread()
		require.NotNil(t, got)
		assert.Equal(t, th.ID, got.ID)
	})

	t.Run("SelectedDraftMessage returns nil on task item", func(t *testing.T) {
		v, _ := newReviewPanel(t)
		assert.Nil(t, v.SelectedDraftMessage())
	})

	t.Run("SelectedDraftMessage returns message on draft thread item", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: 1}
		require.NoError(t, s.CreateTask(task))
		dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
		require.NoError(t, err)
		dm, err := s.AddDraftMessage(dt.ID, ticket.ID, false, "human:aidan", "draft comment")
		require.NoError(t, err)

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Expand task to reveal draft thread item.
		v = sendKey(v, "enter")
		// Cursor should now be on task item; move to draft thread item.
		v = sendKey(v, "j")

		got := v.SelectedDraftMessage()
		require.NotNil(t, got)
		assert.Equal(t, dm.ID, got.ID)
	})

	t.Run("HunkContext returns empty strings when no lines", func(t *testing.T) {
		v, _ := newReviewPanel(t)
		// A ticket with no task selected produces lines but none with hunk context.
		fp, hh := v.HunkContext()
		assert.Empty(t, fp)
		assert.Empty(t, hh)
	})
}

func TestReviewPanel_View(t *testing.T) {
	t.Run("selected item is highlighted with reverse", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "My Task", Position: 1}
		require.NoError(t, s.CreateTask(task))

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		out := v.View()
		// The reverse ANSI sequence must appear somewhere in the output.
		assert.Contains(t, out, "\033[7m", "selected item should have reverse video ANSI sequence")
	})

	t.Run("task with no commit hash shows gray message in right pane", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task No Commit", Position: 1}
		require.NoError(t, s.CreateTask(task))

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		out := v.View()
		assert.Contains(t, out, "(task does not have a commit)")
	})

	t.Run("task with commit hash but no repo_path shows message", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task With Hash", Position: 1, CommitHash: "abc1234"}
		require.NoError(t, s.CreateTask(task))

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		out := v.View()
		assert.Contains(t, out, "(no repo_path configured for this ticket)")
	})

	t.Run("thread with staged resolve shows arrow label", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: 1}
		require.NoError(t, s.CreateTask(task))
		th, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th.ID, "human:aidan", "msg")
		require.NoError(t, err)
		require.NoError(t, s.SetDraftAction(th.ID, ticket.ID, model.DraftActionResolve))

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		// Expand task to see thread.
		v = sendKey(v, "enter")

		out := stripANSI(v.View())
		assert.Contains(t, out, "[→resolved]")
	})

	t.Run("thread count suffix shows correct counts", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: 1}
		require.NoError(t, s.CreateTask(task))

		// 2 open threads.
		th1, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th1.ID, "human:aidan", "open 1")
		require.NoError(t, err)
		th2, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th2.ID, "human:aidan", "open 2")
		require.NoError(t, err)

		// 1 resolved thread.
		th3, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th3.ID, "agent:claude", "resolved")
		require.NoError(t, err)
		_, err = s.DB().Exec(`UPDATE comment_threads SET status='resolved' WHERE id=?`, th3.ID)
		require.NoError(t, err)

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		out := stripANSI(v.View())
		assert.Contains(t, out, "2●")
		assert.Contains(t, out, "1✓")
	})

	t.Run("leftListOffset scrolls to show selected item", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		for i := 1; i <= 10; i++ {
			task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: i}
			require.NoError(t, s.CreateTask(task))
		}

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		// Small height (bodyH = height-4 = 5) to force scrolling.
		v.SetSize(120, 9)

		bodyH := v.bodyH()
		// Navigate to item index 7.
		for i := 0; i < 7; i++ {
			v = sendKey(v, "j")
		}
		assert.Equal(t, 7, v.leftCursor)
		assert.LessOrEqual(t, v.leftListOffset, 7)
		assert.Less(t, 7, v.leftListOffset+bodyH)
	})

	t.Run("SetSize triggers diff rebuild", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task", Position: 1}
		require.NoError(t, s.CreateTask(task))

		v, err := NewReviewPanelView(s, ticket.ID)
		require.NoError(t, err)
		v.SetSize(80, 30)
		out := v.View()
		_ = out // just verify no panic
		v.SetSize(120, 40)
		out2 := v.View()
		_ = out2
	})
}
