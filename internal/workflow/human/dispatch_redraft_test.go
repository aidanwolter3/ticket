package human

import (
	"io"
	"testing"

	"github.com/aidanwolter/ticket/internal/agent"
	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readyTicketWithTask creates a ticket in ready status with one task.
func readyTicketWithTask(t *testing.T, s *store.Store) *model.Ticket {
	t.Helper()
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	return ticket
}

func TestDispatch_Success_WorktreeType(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("agent.command", "echo agent {}"))
	require.NoError(t, s.ConfigSet("workspace.type", "worktree"))

	repoPath := gitRepo(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft, RepoPath: repoPath}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))

	launcher := agent.NewLauncher(s)
	err := Dispatch(s, ticket.ID, launcher, io.Discard, io.Discard)
	require.NoError(t, err)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusInProgress, got.Status)
}

func TestDispatch_CreateFailure_RevertsToReady(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("agent.command", "echo agent {}"))
	require.NoError(t, s.ConfigSet("workspace.type", "custom"))
	require.NoError(t, s.ConfigSet("workspace.create_command", "exit 1"))
	require.NoError(t, s.ConfigSet("workspace.delete_command", "true"))

	ticket := readyTicketWithTask(t, s)

	launcher := agent.NewLauncher(s)
	err := Dispatch(s, ticket.ID, launcher, io.Discard, io.Discard)
	require.Error(t, err)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, got.Status, "ticket should revert to ready on create failure")
}

func TestDispatch_AgentCommandNotConfigured(t *testing.T) {
	s := newTestStore(t)
	ticket := readyTicketWithTask(t, s)

	launcher := agent.NewLauncher(s)
	err := Dispatch(s, ticket.ID, launcher, io.Discard, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent.command")

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, got.Status)
}

func TestRedraft_Success_FromTearingDown(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.type", "custom"))
	require.NoError(t, s.ConfigSet("workspace.delete_command", "true"))

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.CompleteTask(task.ID))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusTearingDown))

	err := Redraft(s, ticket.ID, io.Discard, io.Discard)
	require.NoError(t, err)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusDraft, got.Status)
}

func TestRedraft_CrashRecovery_FromPreparing(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.type", "custom"))
	require.NoError(t, s.ConfigSet("workspace.delete_command", "true"))

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusPreparing))

	err := Redraft(s, ticket.ID, io.Discard, io.Discard)
	require.NoError(t, err)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusDraft, got.Status, "crash recovery should transition preparing → draft without Delete")
}

func TestRedraft_DeleteFailure_StaysInTearingDown(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.type", "custom"))
	require.NoError(t, s.ConfigSet("workspace.delete_command", "exit 1"))

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "t", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.CompleteTask(task.ID))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusTearingDown))

	err := Redraft(s, ticket.ID, io.Discard, io.Discard)
	require.Error(t, err)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusTearingDown, got.Status, "ticket should stay in tearing_down on delete failure")
}

func TestRedraft_CrashRecovery_RetryFromTearingDown(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.type", "custom"))
	require.NoError(t, s.ConfigSet("workspace.delete_command", "true"))

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "t", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.CompleteTask(task.ID))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusTearingDown))

	err := Redraft(s, ticket.ID, io.Discard, io.Discard)
	require.NoError(t, err)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusDraft, got.Status, "retry from tearing_down should succeed when delete succeeds")
}
