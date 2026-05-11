package views

import (
	"strings"
	"testing"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTicketDetailView(t *testing.T) {
	t.Run("completed task shows bullet completed icon incomplete shows ring", func(t *testing.T) {
		s := newTestStore(t)
		tkt := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
		require.NoError(t, s.CreateTicket(tkt))
		task1 := &model.Task{TicketID: tkt.ID, Title: "Done Task", Position: 1}
		task2 := &model.Task{TicketID: tkt.ID, Title: "Todo Task", Position: 2}
		require.NoError(t, s.CreateTask(task1))
		require.NoError(t, s.CreateTask(task2))
		require.NoError(t, s.CompleteTask(task1.ID))

		v, err := NewTicketDetailView(s, tkt.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		out := stripANSI(v.View())
		// Completed task uses "●", incomplete uses "○".
		assert.Contains(t, out, "●")
		assert.Contains(t, out, "○")
	})

	t.Run("task with threads shows thread count", func(t *testing.T) {
		s := newTestStore(t)
		tkt := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
		require.NoError(t, s.CreateTicket(tkt))
		task := &model.Task{TicketID: tkt.ID, Title: "Task 1", Position: 1}
		require.NoError(t, s.CreateTask(task))

		th1, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th1.ID, "human:aidan", "comment 1")
		require.NoError(t, err)
		th2, err := s.CreateThread(task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddMessage(th2.ID, "agent:claude", "comment 2")
		require.NoError(t, err)

		v, err := NewTicketDetailView(s, tkt.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		out := stripANSI(v.View())
		assert.Contains(t, out, "2 open")
	})

	t.Run("ticket with blocker shows blocker ID in view", func(t *testing.T) {
		s := newTestStore(t)
		blocker := &model.Ticket{Title: "Blocker", Type: model.TypeTicket, Status: model.StatusDraft}
		require.NoError(t, s.CreateTicket(blocker))
		blocked := &model.Ticket{Title: "Blocked", Type: model.TypeTicket, Status: model.StatusDraft, BlockedBy: []string{blocker.ID}}
		require.NoError(t, s.CreateTicket(blocked))

		v, err := NewTicketDetailView(s, blocked.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		out := stripANSI(v.View())
		assert.Contains(t, out, blocker.ID)
	})

	t.Run("note text appears in view", func(t *testing.T) {
		s := newTestStore(t)
		tkt := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
		require.NoError(t, s.CreateTicket(tkt))
		_, err := s.AddNote(tkt.ID, "human:aidan", "important note text")
		require.NoError(t, err)

		v, err := NewTicketDetailView(s, tkt.ID)
		require.NoError(t, err)
		v.SetSize(120, 40)

		out := strings.ToLower(stripANSI(v.View()))
		assert.Contains(t, out, "important note text")
	})
}
