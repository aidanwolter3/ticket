package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCompleteTaskMostRecentCommit_NonGitDir_FallsBackToNoCommit(t *testing.T) {
	s := newTestStore(t)

	ticket := &model.Ticket{
		Title:        "T",
		Type:         model.TypeTicket,
		Status:       model.StatusDraft,
		WorktreePath: os.TempDir(), // not a git repo
	}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))

	err := CompleteTaskMostRecentCommit(s, task.ID)
	require.NoError(t, err, "should complete task without error even when git fails")

	got, err := s.GetTask(task.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.CompletedAt, "task should be marked complete")
	assert.Empty(t, got.CommitHash, "commit hash should be empty when git is not available")
}
