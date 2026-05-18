package human

import (
	"io"
	"strings"
	"testing"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandWorkspace_Create_Success(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.type", "custom"))
	require.NoError(t, s.ConfigSet("workspace.create_command", "echo /tmp/ws-$TICKET_ID"))
	require.NoError(t, s.ConfigSet("workspace.delete_command", "true"))

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft, RepoPath: t.TempDir()}
	require.NoError(t, s.CreateTicket(ticket))

	ws := CommandWorkspace{s: s}
	path, err := ws.Create(ticket.ID, io.Discard, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/ws-"+ticket.ID, path)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/ws-"+ticket.ID, got.WorktreePath)
}

func TestCommandWorkspace_Create_Failure(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.create_command", "exit 1"))
	require.NoError(t, s.ConfigSet("workspace.delete_command", "true"))

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	ws := CommandWorkspace{s: s}
	_, err := ws.Create(ticket.ID, io.Discard, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

func TestCommandWorkspace_Delete_Success(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.delete_command", "true"))

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	ws := CommandWorkspace{s: s}
	require.NoError(t, ws.Delete(ticket.ID, io.Discard, io.Discard))
}

func TestCommandWorkspace_Delete_Failure(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.delete_command", "exit 1"))

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	ws := CommandWorkspace{s: s}
	require.Error(t, ws.Delete(ticket.ID, io.Discard, io.Discard))
}

func TestNewWorkspace_CustomType_MissingDeleteCommand(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.type", "modal"))

	_, err := NewWorkspace(s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace.delete_command")
}

func TestNewWorkspace_WorktreeType_WithCreateCommand(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.type", "worktree"))
	require.NoError(t, s.ConfigSet("workspace.create_command", "echo /tmp/ws"))

	_, err := NewWorkspace(s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace.create_command")
}

func TestNewWorkspace_WorktreeType_WithDeleteCommand(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.type", "worktree"))
	require.NoError(t, s.ConfigSet("workspace.delete_command", "rm -rf /tmp/ws"))

	_, err := NewWorkspace(s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace.delete_command")
}

func TestNewWorkspace_DefaultIsWorktree(t *testing.T) {
	s := newTestStore(t)

	ws, err := NewWorkspace(s)
	require.NoError(t, err)
	_, ok := ws.(WorktreeWorkspace)
	assert.True(t, ok, "default workspace should be WorktreeWorkspace")
}

func TestNewWorkspace_CustomType_ReturnsCommandWorkspace(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.type", "modal"))
	require.NoError(t, s.ConfigSet("workspace.delete_command", "true"))

	ws, err := NewWorkspace(s)
	require.NoError(t, err)
	_, ok := ws.(CommandWorkspace)
	assert.True(t, ok, "custom workspace type should return CommandWorkspace")
}

func TestCommandWorkspace_Create_Idempotency(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("workspace.create_command", "echo /tmp/new-path"))

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	require.NoError(t, s.SetWorktreePath(ticket.ID, "/tmp/existing-path", "", ""))

	ws := CommandWorkspace{s: s}
	path, err := ws.Create(ticket.ID, io.Discard, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/existing-path", path, "should return existing path without running command")
}

func TestLastNonEmptyLine(t *testing.T) {
	assert.Equal(t, "/tmp/foo", lastNonEmptyLine("line1\n\n/tmp/foo\n"))
	assert.Equal(t, "only", lastNonEmptyLine("only"))
	assert.Equal(t, "", lastNonEmptyLine(""))
	assert.Equal(t, "last", lastNonEmptyLine(strings.Join([]string{"a", "b", "last", ""}, "\n")))
}
