package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSchemaMigration(t *testing.T) {
	s := newTestStore(t)
	tables := []string{"tickets", "blocked_by", "comment_threads", "thread_messages", "notes"}
	for _, table := range tables {
		var name string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		require.NoError(t, err, "table %s missing", table)
	}
}

func TestCreateAndGetTicket(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{
		Title:       "Test ticket",
		Description: "A test",
		Type:        model.TypeTicket,
		Status:      model.StatusDraft,
	}
	require.NoError(t, s.CreateTicket(ticket))
	assert.Equal(t, "T-001", ticket.ID)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, "Test ticket", got.Title)
	assert.Equal(t, model.StatusDraft, got.Status)
}

func TestPlanWithChildren(t *testing.T) {
	s := newTestStore(t)
	child1 := &model.Ticket{Title: "Child 1", Type: model.TypeTicket, Status: model.StatusDraft}
	child2 := &model.Ticket{Title: "Child 2", Type: model.TypeTicket, Status: model.StatusDraft}
	child3 := &model.Ticket{Title: "Child 3", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(child1))
	require.NoError(t, s.CreateTicket(child2))
	require.NoError(t, s.CreateTicket(child3))

	plan := &model.Ticket{
		Title:     "Parent plan",
		Type:      model.TypePlan,
		Status:    model.StatusDraft,
		BlockedBy: []string{child1.ID, child2.ID, child3.ID},
	}
	require.NoError(t, s.CreateTicket(plan))

	got, err := s.GetTicket(plan.ID)
	require.NoError(t, err)
	assert.Len(t, got.BlockedBy, 3)
}

func TestThreadAndMessages(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	thread, err := s.CreateThread(ticket.ID)
	require.NoError(t, err)

	_, err = s.AddMessage(thread.ID, "human:aidan", "first message")
	require.NoError(t, err)
	_, err = s.AddMessage(thread.ID, "agent:claude", "reply")
	require.NoError(t, err)

	threads, err := s.GetThreadsForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.Len(t, threads[0].Messages, 2)
	assert.Equal(t, "first message", threads[0].Messages[0].Text)
	assert.Equal(t, "reply", threads[0].Messages[1].Text)
}

func TestNotes(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	_, err := s.AddNote(ticket.ID, "human:aidan", "note 1")
	require.NoError(t, err)
	_, err = s.AddNote(ticket.ID, "agent:claude", "note 2")
	require.NoError(t, err)

	notes, err := s.GetNotesForTicket(ticket.ID)
	require.NoError(t, err)
	assert.Len(t, notes, 2)
	assert.Equal(t, "note 1", notes[0].Text)
}

func TestCascadeDelete(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	thread, err := s.CreateThread(ticket.ID)
	require.NoError(t, err)
	_, err = s.AddMessage(thread.ID, "human:aidan", "msg")
	require.NoError(t, err)
	_, err = s.AddNote(ticket.ID, "human:aidan", "note")
	require.NoError(t, err)

	require.NoError(t, s.DeleteTicket(ticket.ID))

	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM comment_threads WHERE ticket_id=?`, ticket.ID).Scan(&count)
	assert.Equal(t, 0, count)
	s.db.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE thread_id=?`, thread.ID).Scan(&count)
	assert.Equal(t, 0, count)
	s.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE ticket_id=?`, ticket.ID).Scan(&count)
	assert.Equal(t, 0, count)
}

func TestInvalidTicketTransition(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	err := s.TransitionTicket(ticket.ID, model.StatusCompleted, "human")
	assert.Error(t, err)
}

func TestInvalidThreadTransition(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	thread, err := s.CreateThread(ticket.ID)
	require.NoError(t, err)

	// Agent trying to resolve directly
	err = s.TransitionThread(thread.ID, model.ThreadResolved, "agent:claude")
	assert.Error(t, err)
}

func TestAvailableWork(t *testing.T) {
	s := newTestStore(t)

	blocker := &model.Ticket{Title: "Blocker", Type: model.TypeTicket, Status: model.StatusReady}
	require.NoError(t, s.CreateTicket(blocker))

	blocked := &model.Ticket{Title: "Blocked", Type: model.TypeTicket, Status: model.StatusReady, BlockedBy: []string{blocker.ID}}
	require.NoError(t, s.CreateTicket(blocked))

	free := &model.Ticket{Title: "Free", Type: model.TypeTicket, Status: model.StatusReady}
	require.NoError(t, s.CreateTicket(free))

	plan := &model.Ticket{Title: "Plan", Type: model.TypePlan, Status: model.StatusReady}
	require.NoError(t, s.CreateTicket(plan))

	work, err := s.AvailableWork()
	require.NoError(t, err)
	ids := ticketIDs(work)
	assert.Contains(t, ids, blocker.ID)
	assert.Contains(t, ids, free.ID)
	assert.NotContains(t, ids, blocked.ID)
	assert.NotContains(t, ids, plan.ID)

	// complete the blocker; blocked should now appear
	_, err = s.db.Exec(`UPDATE tickets SET status='completed' WHERE id=?`, blocker.ID)
	require.NoError(t, err)

	work, err = s.AvailableWork()
	require.NoError(t, err)
	assert.Contains(t, ticketIDs(work), blocked.ID)
}

func TestReviewQueue(t *testing.T) {
	s := newTestStore(t)

	// Full stack in review
	t1 := &model.Ticket{Title: "S1-T1", Type: model.TypeTicket, Status: model.StatusInReview, StackID: "s1"}
	t2 := &model.Ticket{Title: "S1-T2", Type: model.TypeTicket, Status: model.StatusInReview, StackID: "s1"}
	require.NoError(t, s.CreateTicket(t1))
	require.NoError(t, s.CreateTicket(t2))

	// Mixed stack (not in queue)
	t3 := &model.Ticket{Title: "S2-T1", Type: model.TypeTicket, Status: model.StatusInReview, StackID: "s2"}
	t4 := &model.Ticket{Title: "S2-T2", Type: model.TypeTicket, Status: model.StatusInProgress, StackID: "s2"}
	require.NoError(t, s.CreateTicket(t3))
	require.NoError(t, s.CreateTicket(t4))

	// Standalone in_review
	solo := &model.Ticket{Title: "Solo", Type: model.TypeTicket, Status: model.StatusInReview}
	require.NoError(t, s.CreateTicket(solo))

	q, err := s.ReviewQueue()
	require.NoError(t, err)
	assert.Contains(t, stackIDs(q.Stacks), "s1")
	assert.NotContains(t, stackIDs(q.Stacks), "s2")
	assert.Len(t, q.Standalone, 1)
	assert.Equal(t, solo.ID, q.Standalone[0].ID)
}

func TestTicketHierarchy(t *testing.T) {
	s := newTestStore(t)
	plan := &model.Ticket{Title: "Plan", Type: model.TypePlan, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(plan))
	child := &model.Ticket{Title: "Child", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(child))
	plan.BlockedBy = []string{child.ID}
	require.NoError(t, s.UpdateTicket(plan))

	tickets, err := s.TicketHierarchy()
	require.NoError(t, err)
	assert.Len(t, tickets, 2)
}

func TestBlockingTickets(t *testing.T) {
	s := newTestStore(t)
	blocker := &model.Ticket{Title: "Blocker", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(blocker))
	dependent := &model.Ticket{Title: "Dependent", Type: model.TypeTicket, Status: model.StatusDraft, BlockedBy: []string{blocker.ID}}
	require.NoError(t, s.CreateTicket(dependent))

	blocking, err := s.BlockingTickets(blocker.ID)
	require.NoError(t, err)
	assert.Len(t, blocking, 1)
	assert.Equal(t, dependent.ID, blocking[0].ID)
}

func TestOsEnvDBPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "tickets.db")
	s, err := Open(path)
	require.NoError(t, err)
	defer s.Close()
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr)
}

func ticketIDs(tickets []*model.Ticket) []string {
	ids := make([]string, len(tickets))
	for i, t := range tickets {
		ids[i] = t.ID
	}
	return ids
}

func stackIDs(stacks map[string][]*model.Ticket) []string {
	var ids []string
	for k := range stacks {
		ids = append(ids, k)
	}
	return ids
}
