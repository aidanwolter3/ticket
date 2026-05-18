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
	tables := []string{"tickets", "blocked_by", "comment_threads", "thread_messages", "notes", "config"}
	for _, table := range tables {
		var name string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		require.NoError(t, err, "table %s missing", table)
	}
	// Verify repo_path column exists.
	hasRepo, err := s.hasColumn("tickets", "repo_path")
	require.NoError(t, err)
	require.True(t, hasRepo, "repo_path column missing from tickets")
}

func TestMigrate(t *testing.T) {
	// Open a fresh DB — migration2 should run automatically.
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	// config table must exist.
	var name string
	err = s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='config'`).Scan(&name)
	require.NoError(t, err, "config table missing")

	// repo_path column must exist.
	hasRepo, err := s.hasColumn("tickets", "repo_path")
	require.NoError(t, err)
	require.True(t, hasRepo, "repo_path column missing")

	// approved and merged are valid statuses.
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.CompleteTask(task.ID))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusApproved))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusMerged))

	// completed status is no longer valid in the DB.
	ticket2 := &model.Ticket{Title: "T2", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket2))
	_, dbErr := s.db.Exec(`UPDATE tickets SET status='completed' WHERE id=?`, ticket2.ID)
	require.Error(t, dbErr, "completed status should be rejected by CHECK constraint")
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

func TestTicketWithChildren(t *testing.T) {
	s := newTestStore(t)
	child1 := &model.Ticket{Title: "Child 1", Type: model.TypeTicket, Status: model.StatusDraft}
	child2 := &model.Ticket{Title: "Child 2", Type: model.TypeTicket, Status: model.StatusDraft}
	child3 := &model.Ticket{Title: "Child 3", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(child1))
	require.NoError(t, s.CreateTicket(child2))
	require.NoError(t, s.CreateTicket(child3))

	parent := &model.Ticket{
		Title:     "Parent ticket",
		Type:      model.TypeTicket,
		Status:    model.StatusDraft,
		BlockedBy: []string{child1.ID, child2.ID, child3.ID},
	}
	require.NoError(t, s.CreateTicket(parent))

	got, err := s.GetTicket(parent.ID)
	require.NoError(t, err)
	assert.Len(t, got.BlockedBy, 3)
}

func TestThreadAndMessages(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))

	thread, err := s.CreateThread(task.ID, "", "")
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

	blocker := &model.Ticket{Title: "B", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(blocker))
	require.NoError(t, s.AddBlocker(ticket.ID, blocker.ID))

	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))

	thread, err := s.CreateThread(task.ID, "", "")
	require.NoError(t, err)
	_, err = s.AddMessage(thread.ID, "human:aidan", "msg")
	require.NoError(t, err)
	_, err = s.AddNote(ticket.ID, "human:aidan", "note")
	require.NoError(t, err)

	require.NoError(t, s.DeleteTicket(ticket.ID))

	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE ticket_id=?`, ticket.ID).Scan(&count)
	assert.Equal(t, 0, count)
	s.db.QueryRow(`SELECT COUNT(*) FROM comment_threads WHERE task_id=?`, task.ID).Scan(&count)
	assert.Equal(t, 0, count)
	s.db.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE thread_id=?`, thread.ID).Scan(&count)
	assert.Equal(t, 0, count)
	s.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE ticket_id=?`, ticket.ID).Scan(&count)
	assert.Equal(t, 0, count)
	s.db.QueryRow(`SELECT COUNT(*) FROM blocked_by WHERE ticket_id=?`, ticket.ID).Scan(&count)
	assert.Equal(t, 0, count)
}

func TestDeleteNonDraftTicketRejected(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))

	err := s.DeleteTicket(ticket.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only draft tickets may be deleted")
}

func TestInvalidTicketTransition(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	// draft → merged is not a valid transition.
	err := s.TransitionTicket(ticket.ID, model.StatusMerged)
	assert.Error(t, err)
}

func TestTransitionPreconditions(t *testing.T) {
	t.Run("draft→ready requires at least one task", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
		require.NoError(t, s.CreateTicket(ticket))

		err := s.TransitionTicket(ticket.ID, model.StatusReady)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no tasks")

		task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
		require.NoError(t, s.CreateTask(task))
		require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	})

	t.Run("in_progress→in_review requires all tasks complete", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
		require.NoError(t, s.CreateTask(task))
		require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
		require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))

		err := s.TransitionTicket(ticket.ID, model.StatusInReview)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incomplete task")

		require.NoError(t, s.CompleteTask(task.ID))
		require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview))
	})

	t.Run("in_review→approved requires all tasks complete", func(t *testing.T) {
		s := newTestStore(t)
		ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
		require.NoError(t, s.CreateTicket(ticket))
		task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
		require.NoError(t, s.CreateTask(task))
		require.NoError(t, s.CompleteTask(task.ID))
		require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
		require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
		require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview))

		// Add an incomplete task to simulate an amendment being in progress.
		amendment := &model.Task{TicketID: ticket.ID, Title: "Amendment", Position: 2, Round: 2}
		require.NoError(t, s.CreateTask(amendment))

		err := s.TransitionTicket(ticket.ID, model.StatusApproved)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incomplete task")

		require.NoError(t, s.CompleteTask(amendment.ID))
		require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusApproved))
	})

	t.Run("ready→in_progress blocked by non-approved/merged tickets", func(t *testing.T) {
		s := newTestStore(t)
		blocker := &model.Ticket{Title: "Blocker", Type: model.TypeTicket, Status: model.StatusReady}
		require.NoError(t, s.CreateTicket(blocker))
		ticket := &model.Ticket{Title: "Blocked", Type: model.TypeTicket, Status: model.StatusReady, BlockedBy: []string{blocker.ID}}
		require.NoError(t, s.CreateTicket(ticket))

		err := s.TransitionTicket(ticket.ID, model.StatusInProgress)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "blocked by")

		// approved blocker satisfies the check
		_, err = s.db.Exec(`UPDATE tickets SET status='approved' WHERE id=?`, blocker.ID)
		require.NoError(t, err)
		require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
	})
}

func TestInvalidThreadTransition(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))

	thread, err := s.CreateThread(task.ID, "", "")
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

	work, err := s.AvailableWork()
	require.NoError(t, err)
	ids := ticketIDs(work)
	assert.Contains(t, ids, blocker.ID)
	assert.Contains(t, ids, free.ID)
	assert.NotContains(t, ids, blocked.ID)

	// approve the blocker; blocked should now appear
	_, err = s.db.Exec(`UPDATE tickets SET status='approved' WHERE id=?`, blocker.ID)
	require.NoError(t, err)

	work, err = s.AvailableWork()
	require.NoError(t, err)
	assert.Contains(t, ticketIDs(work), blocked.ID)
}

func TestReviewQueue(t *testing.T) {
	s := newTestStore(t)

	r1 := &model.Ticket{Title: "R1", Type: model.TypeTicket, Status: model.StatusInReview}
	r2 := &model.Ticket{Title: "R2", Type: model.TypeTicket, Status: model.StatusInReview}
	require.NoError(t, s.CreateTicket(r1))
	require.NoError(t, s.CreateTicket(r2))

	other := &model.Ticket{Title: "Other", Type: model.TypeTicket, Status: model.StatusInProgress}
	require.NoError(t, s.CreateTicket(other))

	q, err := s.ReviewQueue()
	require.NoError(t, err)
	assert.Len(t, q.Tickets, 2)
	assert.NotContains(t, ticketIDs(q.Tickets), other.ID)
}

func TestTicketHierarchy(t *testing.T) {
	s := newTestStore(t)
	parent := &model.Ticket{Title: "Parent", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(parent))
	child := &model.Ticket{Title: "Child", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(child))
	parent.BlockedBy = []string{child.ID}
	require.NoError(t, s.UpdateTicket(parent))

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

func TestAddBlocker(t *testing.T) {
	s := newTestStore(t)
	a := &model.Ticket{Title: "A", Type: model.TypeTicket, Status: model.StatusDraft}
	b := &model.Ticket{Title: "B", Type: model.TypeTicket, Status: model.StatusDraft}
	c := &model.Ticket{Title: "C", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(a))
	require.NoError(t, s.CreateTicket(b))
	require.NoError(t, s.CreateTicket(c))

	// Self-block
	assert.Error(t, s.AddBlocker(a.ID, a.ID))

	// Non-existent ticket
	assert.Error(t, s.AddBlocker(a.ID, "T-999"))

	// Valid: b blocked by a
	require.NoError(t, s.AddBlocker(b.ID, a.ID))
	got, err := s.GetTicket(b.ID)
	require.NoError(t, err)
	assert.Contains(t, got.BlockedBy, a.ID)

	// Cycle: a blocked by b would create a→b→a cycle
	assert.Error(t, s.AddBlocker(a.ID, b.ID))

	// Longer cycle: c blocked by b, then a blocked by c → a→b→c→a
	require.NoError(t, s.AddBlocker(c.ID, b.ID))
	assert.Error(t, s.AddBlocker(a.ID, c.ID))
}

func TestRemoveBlocker(t *testing.T) {
	s := newTestStore(t)
	a := &model.Ticket{Title: "A", Type: model.TypeTicket, Status: model.StatusDraft}
	b := &model.Ticket{Title: "B", Type: model.TypeTicket, Status: model.StatusDraft, BlockedBy: nil}
	require.NoError(t, s.CreateTicket(a))
	require.NoError(t, s.CreateTicket(b))

	// Remove non-existent relationship
	assert.Error(t, s.RemoveBlocker(b.ID, a.ID))

	// Add then remove
	require.NoError(t, s.AddBlocker(b.ID, a.ID))
	require.NoError(t, s.RemoveBlocker(b.ID, a.ID))
	got, err := s.GetTicket(b.ID)
	require.NoError(t, err)
	assert.NotContains(t, got.BlockedBy, a.ID)
}

func TestConfigList(t *testing.T) {
	s := newTestStore(t)

	// Empty store returns empty map.
	m, err := s.ConfigList()
	require.NoError(t, err)
	assert.Empty(t, m)

	require.NoError(t, s.ConfigSet("worktrees", "false"))
	require.NoError(t, s.ConfigSet("other", "val"))

	m, err = s.ConfigList()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"worktrees": "false", "other": "val"}, m)
}

func TestTaskRoundField(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	// Default round is 1.
	task1 := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task1))
	assert.Equal(t, 1, task1.Round)

	got, err := s.GetTask(task1.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Round)

	// Explicit round > 1.
	task2 := &model.Task{TicketID: ticket.ID, Title: "Amendment", Position: 2, Round: 2}
	require.NoError(t, s.CreateTask(task2))
	assert.Equal(t, 2, task2.Round)

	got2, err := s.GetTask(task2.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got2.Round)

	// GetTasksForTicket preserves round.
	tasks, err := s.GetTasksForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	assert.Equal(t, 1, tasks[0].Round)
	assert.Equal(t, 2, tasks[1].Round)
}

func TestTaskRoundSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "round.db")

	s1, err := Open(dbPath)
	require.NoError(t, err)

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s1.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Amendment", Position: 1, Round: 3}
	require.NoError(t, s1.CreateTask(task))
	s1.Close()

	s2, err := Open(dbPath)
	require.NoError(t, err)
	defer s2.Close()

	got, err := s2.GetTask(task.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, got.Round)
}

func TestDraftStatePersists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	s1, err := Open(dbPath)
	require.NoError(t, err)

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s1.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s1.CreateTask(task))

	// Real thread for staged action/reply.
	realThread, err := s1.CreateThread(task.ID, "", "")
	require.NoError(t, err)

	// Draft thread with a message.
	dt, err := s1.CreateDraftThread(ticket.ID, task.ID, "", "")
	require.NoError(t, err)
	_, err = s1.AddDraftMessage(dt.ID, ticket.ID, false, "human", "review comment")
	require.NoError(t, err)

	// Draft reply to the real thread.
	_, err = s1.AddDraftMessage(realThread.ID, ticket.ID, true, "human", "reply text")
	require.NoError(t, err)

	// Staged resolve.
	err = s1.SetDraftAction(realThread.ID, ticket.ID, model.DraftActionResolve)
	require.NoError(t, err)

	s1.Close()

	// Reopen the store.
	s2, err := Open(dbPath)
	require.NoError(t, err)
	defer s2.Close()

	state, err := s2.GetDraftState(ticket.ID)
	require.NoError(t, err)
	require.Len(t, state.NewThreads, 1)
	require.Len(t, state.NewThreads[0].Messages, 1)
	assert.Equal(t, "review comment", state.NewThreads[0].Messages[0].Text)
	require.Len(t, state.Replies, 1)
	assert.Equal(t, "reply text", state.Replies[0].Text)
	require.Len(t, state.Actions, 1)
	assert.Equal(t, model.DraftActionResolve, state.Actions[0].Action)
	assert.Equal(t, realThread.ID, state.Actions[0].ThreadID)
}

func TestFlushDraftState(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))

	// Real threads.
	openThread, err := s.CreateThread(task.ID, "", "")
	require.NoError(t, err)
	resolvedThread, err := s.CreateThread(task.ID, "", "")
	require.NoError(t, err)
	_, err = s.db.Exec(`UPDATE comment_threads SET status='resolved' WHERE id=?`, resolvedThread.ID)
	require.NoError(t, err)

	// Draft new thread.
	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
	require.NoError(t, err)
	_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human", "new comment")
	require.NoError(t, err)

	// Draft reply to open thread.
	_, err = s.AddDraftMessage(openThread.ID, ticket.ID, true, "human", "this needs work")
	require.NoError(t, err)

	// Stage reopen on resolved thread.
	err = s.SetDraftAction(resolvedThread.ID, ticket.ID, model.DraftActionReopen)
	require.NoError(t, err)

	_, flushErr := s.FlushDraftState(ticket.ID)
	require.NoError(t, flushErr)

	// Draft state must be cleared.
	state, err := s.GetDraftState(ticket.ID)
	require.NoError(t, err)
	assert.True(t, state.IsEmpty())

	// Real threads: open thread → needs_attention (got a reply).
	threads, err := s.GetThreadsForTask(task.ID)
	require.NoError(t, err)
	assert.Len(t, threads, 3) // openThread + resolvedThread + newly created

	statuses := make(map[string]model.ThreadStatus)
	for _, th := range threads {
		statuses[th.ID] = th.Status
	}
	assert.Equal(t, model.ThreadNeedsAttention, statuses[openThread.ID])
	assert.Equal(t, model.ThreadOpen, statuses[resolvedThread.ID])
}

func TestDraftActionToggle(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	thread, err := s.CreateThread(task.ID, "", "")
	require.NoError(t, err)

	// Set resolve, then clear it.
	require.NoError(t, s.SetDraftAction(thread.ID, ticket.ID, model.DraftActionResolve))
	state, err := s.GetDraftState(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DraftActionResolve, state.ActionFor(thread.ID))

	require.NoError(t, s.ClearDraftAction(thread.ID))
	state, err = s.GetDraftState(ticket.ID)
	require.NoError(t, err)
	assert.Empty(t, state.ActionFor(thread.ID))
}

func TestDraftMessageEditDelete(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
	require.NoError(t, err)

	msg, err := s.AddDraftMessage(dt.ID, ticket.ID, false, "human", "original text")
	require.NoError(t, err)

	require.NoError(t, s.UpdateDraftMessage(msg.ID, "edited text"))
	state, err := s.GetDraftState(ticket.ID)
	require.NoError(t, err)
	require.Len(t, state.NewThreads[0].Messages, 1)
	assert.Equal(t, "edited text", state.NewThreads[0].Messages[0].Text)

	// Adding a second message so the thread survives single-message deletion
	msg2, err := s.AddDraftMessage(dt.ID, ticket.ID, false, "human", "second message")
	require.NoError(t, err)

	require.NoError(t, s.DeleteDraftMessage(msg2.ID))
	state, err = s.GetDraftState(ticket.ID)
	require.NoError(t, err)
	// Thread still exists with msg (edited).
	require.Len(t, state.NewThreads, 1)
	assert.Len(t, state.NewThreads[0].Messages, 1)
}

func TestDraftThreadHunkAnchor(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))

	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "internal/foo/bar.go", "@@ -10,7 +10,8 @@")
	require.NoError(t, err)
	_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human:alice", "rename this variable")
	require.NoError(t, err)

	state, err := s.GetDraftState(ticket.ID)
	require.NoError(t, err)
	require.Len(t, state.NewThreads, 1)
	assert.Equal(t, "internal/foo/bar.go", state.NewThreads[0].FilePath)
	assert.Equal(t, "@@ -10,7 +10,8 @@", state.NewThreads[0].HunkHeader)
	assert.Equal(t, task.ID, state.NewThreads[0].TaskID)
	require.Len(t, state.NewThreads[0].Messages, 1)
	assert.Equal(t, "rename this variable", state.NewThreads[0].Messages[0].Text)
}

func TestFlushDraftState_PreservesHunkAnchor(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusInReview}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))

	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "cmd/main.go", "@@ -5,3 +5,4 @@")
	require.NoError(t, err)
	_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human:alice", "extract this into a helper")
	require.NoError(t, err)

	_, err = s.FlushDraftState(ticket.ID)
	require.NoError(t, err)

	threads, err := s.GetThreadsForTask(task.ID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.Equal(t, "cmd/main.go", threads[0].FilePath)
	assert.Equal(t, "@@ -5,3 +5,4 @@", threads[0].HunkHeader)
	assert.Equal(t, model.ThreadNeedsAttention, threads[0].Status)
}

func TestUpdateTask_CommitHash(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	assert.Empty(t, task.CommitHash)

	// Simulate "ticket task set-commit" by calling UpdateTask with a hash.
	task.CommitHash = "abc1234def5678"
	require.NoError(t, s.UpdateTask(task))

	got, err := s.GetTask(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "abc1234def5678", got.CommitHash)

	// Updating other fields should not clobber the hash.
	task.Title = "Renamed task"
	require.NoError(t, s.UpdateTask(task))

	got, err = s.GetTask(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "abc1234def5678", got.CommitHash)
	assert.Equal(t, "Renamed task", got.Title)
}

func TestDeleteDraftMessageAutoDeletesEmptyThread(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))

	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
	require.NoError(t, err)

	msg, err := s.AddDraftMessage(dt.ID, ticket.ID, false, "human", "only message")
	require.NoError(t, err)

	require.NoError(t, s.DeleteDraftMessage(msg.ID))

	_, err = s.GetDraftThread(dt.ID)
	require.Error(t, err, "draft thread should be auto-deleted when its only message is deleted")
}

func TestAgentSessionCRUD(t *testing.T) {
	s := newTestStore(t)

	// Table must exist.
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='agent_sessions'`).Scan(&name)
	require.NoError(t, err, "agent_sessions table missing")

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	sess, err := s.CreateAgentSession(ticket.ID, 9999, "/tmp/out.log")
	require.NoError(t, err)
	assert.Equal(t, ticket.ID, sess.TicketID)
	assert.Equal(t, 9999, sess.PID)
	assert.Equal(t, model.AgentRunning, sess.State)

	got, err := s.GetAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, sess.ID, got.ID)

	require.NoError(t, s.UpdateAgentSessionState(sess.ID, model.AgentWaiting))
	got, err = s.GetAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, model.AgentWaiting, got.State)

	require.NoError(t, s.TerminateAllAgentSessions())
	got, err = s.GetAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "terminated session should not be returned")
}

func TestUpdateTask(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	task := &model.Task{TicketID: ticket.ID, Title: "Original title", Description: "Original desc", VerifiableResult: "vr1", Position: 1}
	require.NoError(t, s.CreateTask(task))

	// Update only title.
	task.Title = "New title"
	require.NoError(t, s.UpdateTask(task))

	got, err := s.GetTask(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "New title", got.Title)
	assert.Equal(t, "Original desc", got.Description)
	assert.Equal(t, "vr1", got.VerifiableResult)
	assert.Equal(t, 1, got.Position)
	assert.Equal(t, task.ID, got.ID)

	// Update description and verifiable result together.
	task.Description = "New desc"
	task.VerifiableResult = "vr2"
	require.NoError(t, s.UpdateTask(task))

	got, err = s.GetTask(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "New title", got.Title)
	assert.Equal(t, "New desc", got.Description)
	assert.Equal(t, "vr2", got.VerifiableResult)
	assert.Equal(t, 1, got.Position)
	assert.Equal(t, task.ID, got.ID)
}

func TestMoveTask(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	t1 := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	t2 := &model.Task{TicketID: ticket.ID, Title: "Task 2", Position: 2}
	t3 := &model.Task{TicketID: ticket.ID, Title: "Task 3", Position: 3}
	require.NoError(t, s.CreateTask(t1))
	require.NoError(t, s.CreateTask(t2))
	require.NoError(t, s.CreateTask(t3))

	// Move t3 to position 1.
	require.NoError(t, s.MoveTask(t3.ID, 1))
	tasks, err := s.GetTasksForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 3)
	assert.Equal(t, t3.ID, tasks[0].ID)
	assert.Equal(t, 1, tasks[0].Position)
	assert.Equal(t, t1.ID, tasks[1].ID)
	assert.Equal(t, 2, tasks[1].Position)
	assert.Equal(t, t2.ID, tasks[2].ID)
	assert.Equal(t, 3, tasks[2].Position)

	// Move t3 (now at position 1) to position 2.
	require.NoError(t, s.MoveTask(t3.ID, 2))
	tasks, err = s.GetTasksForTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, t1.ID, tasks[0].ID)
	assert.Equal(t, t3.ID, tasks[1].ID)
	assert.Equal(t, t2.ID, tasks[2].ID)

	// Moving to same position is a no-op.
	require.NoError(t, s.MoveTask(t3.ID, 2))

	// Out-of-range position returns error.
	err = s.MoveTask(t3.ID, 99)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")

	err = s.MoveTask(t3.ID, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func ticketIDs(tickets []*model.Ticket) []string {
	ids := make([]string, len(tickets))
	for i, t := range tickets {
		ids[i] = t.ID
	}
	return ids
}

func TestCreateTicket_RejectsFeatureBranch(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{
		Title:         "should fail",
		Type:          model.TypeTicket,
		Status:        model.StatusDraft,
		FeatureBranch: "feat/t-001",
	}
	err := s.CreateTicket(ticket)
	require.Error(t, err, "CreateTicket with non-empty FeatureBranch must return an error")
}

func TestMigration9_PreparingStatusAllowed(t *testing.T) {
	s := newTestStore(t)

	// Insert a ticket with status="preparing" directly via SQL — bypasses model validation.
	ticket := &model.Ticket{Title: "prep ticket", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	_, err := s.db.Exec(`UPDATE tickets SET status='preparing' WHERE id=?`, ticket.ID)
	require.NoError(t, err, "status=preparing should be accepted by CHECK constraint after migration9")

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusPreparing, got.Status)
}


