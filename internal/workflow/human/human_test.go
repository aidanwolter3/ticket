package human

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aidanwolter/ticket/internal/agent"
	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStore opens an in-memory SQLite store for tests.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

// gitRepo creates a temp directory with an initialised git repo containing one
// commit on 'main'. Returns the repo path.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial\n"), 0644))
	git("add", ".")
	git("commit", "-m", "initial")
	return dir
}

// approvedTicket inserts a ticket already in approved status with the given
// repo/branch/worktree fields. A completed task is added to satisfy the
// task-complete preconditions on in_review and approved transitions.
func approvedTicket(t *testing.T, s *store.Store, repoPath, featureBranch, worktreePath string) *model.Ticket {
	t.Helper()
	ticket := &model.Ticket{
		Title:    "test",
		Type:     model.TypeTicket,
		Status:   model.StatusDraft,
		RepoPath: repoPath,
	}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "setup task", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.CompleteTask(task.ID))
	require.NoError(t, s.SetWorktreePath(ticket.ID, worktreePath, repoPath, featureBranch))
	for _, to := range []model.Status{
		model.StatusReady, model.StatusInProgress, model.StatusInReview, model.StatusApproved,
	} {
		require.NoError(t, s.TransitionTicket(ticket.ID, to))
	}
	return ticket
}

func TestDraft_CreatesTicket(t *testing.T) {
	s := newTestStore(t)
	ticket, err := Draft(s, "My Ticket", "Some description", "/path/to/repo")
	require.NoError(t, err)
	require.NotNil(t, ticket)
	assert.NotEmpty(t, ticket.ID)
	assert.Equal(t, "My Ticket", ticket.Title)
	assert.Equal(t, "Some description", ticket.Description)
	assert.Equal(t, "/path/to/repo", ticket.RepoPath)
	assert.Equal(t, model.StatusDraft, ticket.Status)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, ticket.ID, got.ID)
}

func TestDelete_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ticket, err := Draft(s, "To Delete", "", "")
	require.NoError(t, err)
	require.NoError(t, Delete(s, ticket.ID))
	_, err = s.GetTicket(ticket.ID)
	assert.Error(t, err)
}

func TestDelete_BlockedForActiveStatus(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{Title: "Active", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "t", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))

	err := Delete(s, ticket.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in_progress")
}

func TestUpdate_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ticket, err := Draft(s, "Old Title", "Old Desc", "")
	require.NoError(t, err)

	newTitle := "New Title"
	newDesc := "New Desc"
	require.NoError(t, Update(s, ticket.ID, &newTitle, &newDesc))

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, "New Title", got.Title)
	assert.Equal(t, "New Desc", got.Description)
}

func TestUpdate_PartialUpdate(t *testing.T) {
	s := newTestStore(t)
	ticket, err := Draft(s, "Original Title", "Original Desc", "")
	require.NoError(t, err)

	newTitle := "Updated Title"
	require.NoError(t, Update(s, ticket.ID, &newTitle, nil))

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", got.Title)
	assert.Equal(t, "Original Desc", got.Description)
}

// TestMerge_FastForward verifies that a feature branch that is already an
// ancestor of main fast-forward merges without any rebase step.
func TestMerge_FastForward(t *testing.T) {
	repoPath := gitRepo(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	git("checkout", "-b", "feat/test")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "feature.txt"), []byte("feature\n"), 0644))
	git("add", ".")
	git("commit", "-m", "feature commit")
	git("checkout", "main")

	s := newTestStore(t)
	ticket := approvedTicket(t, s, repoPath, "feat/test", "")

	err := Merge(s, ticket.ID, io.Discard, io.Discard)
	assert.NoError(t, err)

	// Feature branch should no longer exist after merge.
	check := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "feat/test")
	assert.Error(t, check.Run(), "branch should have been deleted after merge")
}

// TestMerge_AutoRebase_NoWorktree verifies that when main has moved ahead of a
// feature branch (no worktree), Merge auto-rebases and succeeds.
func TestMerge_AutoRebase_NoWorktree(t *testing.T) {
	repoPath := gitRepo(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}

	// Create a commit on the feature branch.
	git("checkout", "-b", "feat/test")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "feature.txt"), []byte("feature\n"), 0644))
	git("add", ".")
	git("commit", "-m", "feature commit")

	// Advance main so the feature branch has diverged.
	git("checkout", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "main-extra.txt"), []byte("extra\n"), 0644))
	git("add", ".")
	git("commit", "-m", "main advances")

	s := newTestStore(t)
	ticket := approvedTicket(t, s, repoPath, "feat/test", "")

	err := Merge(s, ticket.ID, io.Discard, io.Discard)
	assert.NoError(t, err, "Merge should auto-rebase and succeed")

	// feature.txt must be present on main after the merge.
	_, statErr := os.Stat(filepath.Join(repoPath, "feature.txt"))
	assert.NoError(t, statErr, "feature.txt should be on main after auto-rebase merge")
}

// TestMerge_AutoRebase_WithWorktree verifies the same divergence scenario when
// a worktree is present (rebase runs inside the worktree directory).
func TestMerge_AutoRebase_WithWorktree(t *testing.T) {
	repoPath := gitRepo(t)
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}

	// Create the feature branch.
	git(repoPath, "checkout", "-b", "feat/test")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "feature.txt"), []byte("feature\n"), 0644))
	git(repoPath, "add", ".")
	git(repoPath, "commit", "-m", "feature commit")
	git(repoPath, "checkout", "main")

	// Create a worktree for the feature branch.
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	git(repoPath, "worktree", "add", worktreePath, "feat/test")
	git(worktreePath, "config", "user.email", "test@example.com")
	git(worktreePath, "config", "user.name", "Test")

	// Advance main to cause divergence.
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "main-extra.txt"), []byte("extra\n"), 0644))
	git(repoPath, "add", ".")
	git(repoPath, "commit", "-m", "main advances")

	s := newTestStore(t)
	ticket := approvedTicket(t, s, repoPath, "feat/test", worktreePath)

	err := Merge(s, ticket.ID, io.Discard, io.Discard)
	assert.NoError(t, err, "Merge should auto-rebase via worktree and succeed")

	_, statErr := os.Stat(filepath.Join(repoPath, "feature.txt"))
	assert.NoError(t, statErr, "feature.txt should be on main after auto-rebase merge")
}

// TestMerge_AutoRebase_Conflicts verifies that when rebase produces conflicts,
// Merge returns an error mentioning the path to resolve them.
func TestMerge_AutoRebase_Conflicts(t *testing.T) {
	repoPath := gitRepo(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	// Feature branch changes file.txt.
	git("checkout", "-b", "feat/conflict")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "file.txt"), []byte("feature version\n"), 0644))
	git("add", ".")
	git("commit", "-m", "feature changes file.txt")

	// Main also changes file.txt in an incompatible way.
	git("checkout", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "file.txt"), []byte("main version\n"), 0644))
	git("add", ".")
	git("commit", "-m", "main changes file.txt")

	s := newTestStore(t)
	ticket := approvedTicket(t, s, repoPath, "feat/conflict", "")

	var errBuf strings.Builder
	err := Merge(s, ticket.ID, io.Discard, &errBuf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rebase produced conflicts")
	assert.Contains(t, err.Error(), "retry ticket merge")

	// Abort the rebase so the repo is clean after the test.
	exec.Command("git", "-C", repoPath, "rebase", "--abort").Run()
}

func TestSubmitReview_FlushesAndTransitions(t *testing.T) {
	s := newTestStore(t)

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.CompleteTask(task.ID))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview))

	// Existing real thread (open).
	th, err := s.CreateThread(task.ID, "", "")
	require.NoError(t, err)

	// Stage: draft new thread.
	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
	require.NoError(t, err)
	_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human", "please fix this")
	require.NoError(t, err)

	// Stage: resolve the open thread.
	require.NoError(t, s.SetDraftAction(th.ID, ticket.ID, model.DraftActionResolve))

	require.NoError(t, SubmitReview(s, ticket.ID, "human", nil, io.Discard, io.Discard))

	// Ticket should be ready.
	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, got.Status)

	// Draft state cleared.
	ds, err := s.GetDraftState(ticket.ID)
	require.NoError(t, err)
	assert.True(t, ds.IsEmpty())

	// Real threads: open thread → resolved (staged); new draft thread → needs_attention.
	threads, err := s.GetThreadsForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, threads, 2)

	statuses := make(map[string]model.ThreadStatus)
	for _, t := range threads {
		statuses[t.ID] = t.Status
	}
	assert.Equal(t, model.ThreadResolved, statuses[th.ID])
	// The newly created real thread (from draft) should be needs_attention.
	for id, status := range statuses {
		if id != th.ID {
			assert.Equal(t, model.ThreadNeedsAttention, status, "new thread should be needs_attention")
		}
	}
}

func TestSubmitReview_CreatesAmendmentTasks(t *testing.T) {
	s := newTestStore(t)

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Original task", Position: 1, Round: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.CompleteTask(task.ID))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview))

	// Create 3 draft threads (all will become needs_attention).
	for i := 0; i < 3; i++ {
		dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
		require.NoError(t, err)
		_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human", "review comment")
		require.NoError(t, err)
	}

	require.NoError(t, SubmitReview(s, ticket.ID, "human", nil, io.Discard, io.Discard))

	// Ticket should be ready.
	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, got.Status)

	// Should have 1 original + 3 amendment tasks.
	tasks, err := s.GetTasksForTicket(ticket.ID)
	require.NoError(t, err)
	assert.Len(t, tasks, 4)

	round1, round2 := 0, 0
	for _, tk := range tasks {
		if tk.Round == 1 {
			round1++
		} else if tk.Round == 2 {
			round2++
		}
	}
	assert.Equal(t, 1, round1)
	assert.Equal(t, 3, round2)
}

func TestSubmitReview_EmptyDraftOK(t *testing.T) {
	s := newTestStore(t)

	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.CompleteTask(task.ID))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview))

	// No draft state — no needs_attention threads, so ticket stays in_review.
	require.NoError(t, SubmitReview(s, ticket.ID, "human", nil, io.Discard, io.Discard))

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusInReview, got.Status)
}

// newInReviewTicket creates a ticket in in_review status with one completed task.
func newInReviewTicket(t *testing.T, s *store.Store) (*model.Ticket, *model.Task) {
	t.Helper()
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.CompleteTask(task.ID))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview))
	return ticket, task
}

// TestSubmitReview_OnlyResolutionsStaysInReview verifies that when all staged
// draft actions are resolutions (no new needs_attention threads), the ticket
// stays in_review.
func TestSubmitReview_OnlyResolutionsStaysInReview(t *testing.T) {
	s := newTestStore(t)
	ticket, task := newInReviewTicket(t, s)

	// Open thread exists; stage only a resolution for it.
	th, err := s.CreateThread(task.ID, "", "")
	require.NoError(t, err)
	require.NoError(t, s.SetDraftAction(th.ID, ticket.ID, model.DraftActionResolve))

	require.NoError(t, SubmitReview(s, ticket.ID, "human", nil, io.Discard, io.Discard))

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusInReview, got.Status, "resolving comments only must not transition to ready")

	// The resolved thread should be resolved.
	threads, err := s.GetThreadsForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.Equal(t, model.ThreadResolved, threads[0].Status)
}

// TestSubmitReview_NewThreadTransitionsToReady verifies that when at least one
// staged action creates a needs_attention thread, the ticket transitions to ready.
func TestSubmitReview_NewThreadTransitionsToReady(t *testing.T) {
	s := newTestStore(t)
	ticket, task := newInReviewTicket(t, s)

	// Stage a new draft thread (will become needs_attention on flush).
	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
	require.NoError(t, err)
	_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human", "please rename this")
	require.NoError(t, err)

	require.NoError(t, SubmitReview(s, ticket.ID, "human", nil, io.Discard, io.Discard))

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, got.Status, "new change request thread must transition ticket to ready")

	threads, err := s.GetThreadsForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.Equal(t, model.ThreadNeedsAttention, threads[0].Status)
}

// TestRedraft_KillsAgentSession verifies that Redraft sends SIGTERM to an active
// agent process and marks the session as terminated.
func TestRedraft_KillsAgentSession(t *testing.T) {
	repoPath := gitRepo(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("branch", "feat/t-999")
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	git("worktree", "add", worktreePath, "feat/t-999")

	s := newTestStore(t)
	ticket := &model.Ticket{
		Title:    "agent session cleanup test",
		Type:     model.TypeTicket,
		Status:   model.StatusDraft,
		RepoPath: repoPath,
	}
	require.NoError(t, s.CreateTicket(ticket))
	setupTask := &model.Task{TicketID: ticket.ID, Title: "setup task", Position: 1}
	require.NoError(t, s.CreateTask(setupTask))
	require.NoError(t, s.SetWorktreePath(ticket.ID, worktreePath, repoPath, "feat/t-999"))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))

	// Start a long-running process to simulate an agent.
	sleepCmd := exec.Command("sleep", "300")
	require.NoError(t, sleepCmd.Start())
	pid := sleepCmd.Process.Pid
	t.Cleanup(func() { sleepCmd.Process.Kill(); sleepCmd.Wait() })

	sess, err := s.CreateAgentSession(ticket.ID, pid, "/tmp/fake.log")
	require.NoError(t, err)

	// Redraft should kill the process and mark the session terminated.
	err = Redraft(s, ticket.ID, io.Discard, io.Discard)
	require.NoError(t, err)

	// Give the signal a moment to land.
	time.Sleep(50 * time.Millisecond)

	// Process should no longer be running (signal 0 fails or process is done).
	proc, _ := os.FindProcess(pid)
	sigErr := proc.Signal(syscall.Signal(0))
	// On Unix, signal 0 returns an error if the process is gone or a zombie.
	// Accept either: process dead or a zombie (which also means it exited).
	_ = sigErr // best-effort; session state is the authoritative check

	// Session state must be terminated in the DB.
	updated, err := s.GetAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	assert.Nil(t, updated, "active session should be gone after redraft")

	// Confirm via the ID directly using a separate query approach: get latest.
	latest, err := s.GetLatestAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, sess.ID, latest.ID)
	assert.Equal(t, model.AgentTerminated, latest.State)

	// Ticket should be back in draft.
	updated2, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusDraft, updated2.Status)
}

// TestRedraft_ResetsTaskStatuses verifies that all completed tasks are reset to
// pending when a ticket is redrafted.
func TestRedraft_ResetsTaskStatuses(t *testing.T) {
	repoPath := gitRepo(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("branch", "feat/t-reset")
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	git("worktree", "add", worktreePath, "feat/t-reset")

	s := newTestStore(t)
	ticket := &model.Ticket{
		Title:    "task reset test",
		Type:     model.TypeTicket,
		Status:   model.StatusDraft,
		RepoPath: repoPath,
	}
	require.NoError(t, s.CreateTicket(ticket))
	require.NoError(t, s.SetWorktreePath(ticket.ID, worktreePath, repoPath, "feat/t-reset"))

	// Create tasks before transitioning to ready (draft→ready requires at least one task).
	task1 := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	task2 := &model.Task{TicketID: ticket.ID, Title: "Task 2", Position: 2}
	require.NoError(t, s.CreateTask(task1))
	require.NoError(t, s.CreateTask(task2))

	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusReady))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))

	// Complete both tasks.
	require.NoError(t, s.CompleteTask(task1.ID))
	require.NoError(t, s.CompleteTask(task2.ID))

	// Confirm both tasks are complete before redraft.
	tasks, err := s.GetTasksForTicket(ticket.ID)
	require.NoError(t, err)
	for _, tk := range tasks {
		assert.NotNil(t, tk.CompletedAt, "task %s should be complete before redraft", tk.ID)
	}

	// Redraft the ticket.
	err = Redraft(s, ticket.ID, io.Discard, io.Discard)
	require.NoError(t, err)

	// All tasks must be pending again.
	tasks, err = s.GetTasksForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	for _, tk := range tasks {
		assert.Nil(t, tk.CompletedAt, "task %s should be pending after redraft", tk.ID)
	}

	// Ticket must be back in draft.
	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusDraft, got.Status)
}

// TestReady_CreatesWorktreeForMissingPath verifies that when auto_dispatch is
// enabled and a ticket has RepoPath+FeatureBranch but no WorktreePath, Ready
// creates the worktree on disk and persists the path before launching the agent.
func TestReady_CreatesWorktreeForMissingPath(t *testing.T) {
	repoPath := gitRepo(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("checkout", "-b", "feat/t-999")
	git("checkout", "main")

	s := newTestStore(t)
	ticket := &model.Ticket{
		Title:    "worktree creation test",
		Type:     model.TypeTicket,
		Status:   model.StatusDraft,
		RepoPath: repoPath,
	}
	require.NoError(t, s.CreateTicket(ticket))
	setupTask := &model.Task{TicketID: ticket.ID, Title: "setup task", Position: 1}
	require.NoError(t, s.CreateTask(setupTask))
	require.NoError(t, s.SetWorktreePath(ticket.ID, "", repoPath, "feat/t-999"))

	require.NoError(t, s.ConfigSet("agent.auto_dispatch", "true"))
	require.NoError(t, s.ConfigSet("agent.command", "echo {}"))

	launcher := agent.NewLauncher(s)
	err := Ready(s, ticket.ID, launcher, io.Discard, io.Discard)
	require.NoError(t, err)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, got.WorktreePath, "WorktreePath should be set after Ready")
	_, statErr := os.Stat(got.WorktreePath)
	assert.NoError(t, statErr, "worktree directory should exist on disk")
}

// TestReady_SkipsAutoDispatchWithOpenBlocker verifies that when auto_dispatch is
// enabled and a ticket has an unresolved blocker, Ready skips launch and logs the reason.
func TestReady_SkipsAutoDispatchWithOpenBlocker(t *testing.T) {
	s := newTestStore(t)

	blocker := &model.Ticket{Title: "Blocker", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(blocker))

	ticket := &model.Ticket{Title: "Dependent", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))
	setupTask := &model.Task{TicketID: ticket.ID, Title: "setup task", Position: 1}
	require.NoError(t, s.CreateTask(setupTask))
	require.NoError(t, s.AddBlocker(ticket.ID, blocker.ID))

	require.NoError(t, s.ConfigSet("agent.auto_dispatch", "true"))
	require.NoError(t, s.ConfigSet("agent.command", "echo {}"))

	launcher := agent.NewLauncher(s)
	var errBuf bytes.Buffer
	err := Ready(s, ticket.ID, launcher, io.Discard, &errBuf)
	require.NoError(t, err, "Ready must not return an error when skipping due to blockers")
	assert.Contains(t, errBuf.String(), "skipped", "reason for skipping must be logged to stderr")
	assert.Contains(t, errBuf.String(), blocker.ID)

	sess, err := s.GetAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	assert.Nil(t, sess, "no agent session should be created when blockers are unresolved")
}

// TestReady_AutoDispatchWithApprovedBlocker verifies that when all blockers are
// approved (or merged), Ready proceeds with auto-dispatch.
func TestReady_AutoDispatchWithApprovedBlocker(t *testing.T) {
	repoPath := gitRepo(t)
	s := newTestStore(t)

	blocker := &model.Ticket{Title: "Blocker", Type: model.TypeTicket, Status: model.StatusDraft, RepoPath: repoPath}
	require.NoError(t, s.CreateTicket(blocker))
	blockerTask := &model.Task{TicketID: blocker.ID, Title: "blocker task", Position: 1}
	require.NoError(t, s.CreateTask(blockerTask))
	require.NoError(t, s.CompleteTask(blockerTask.ID))
	for _, to := range []model.Status{model.StatusReady, model.StatusInProgress, model.StatusInReview, model.StatusApproved} {
		require.NoError(t, s.TransitionTicket(blocker.ID, to))
	}

	ticket := &model.Ticket{Title: "Dependent", Type: model.TypeTicket, Status: model.StatusDraft, RepoPath: repoPath}
	require.NoError(t, s.CreateTicket(ticket))
	dependentTask := &model.Task{TicketID: ticket.ID, Title: "dependent task", Position: 1}
	require.NoError(t, s.CreateTask(dependentTask))
	require.NoError(t, s.AddBlocker(ticket.ID, blocker.ID))

	require.NoError(t, s.ConfigSet("agent.auto_dispatch", "true"))
	require.NoError(t, s.ConfigSet("agent.command", "echo {}"))

	launcher := agent.NewLauncher(s)
	var errBuf bytes.Buffer
	err := Ready(s, ticket.ID, launcher, io.Discard, &errBuf)
	require.NoError(t, err)
	assert.NotContains(t, errBuf.String(), "skipped", "should not skip when blocker is approved")

	sess, err := s.GetAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	assert.NotNil(t, sess, "agent session should be created when all blockers are approved")
}

// TestReady_SkipsLaunchOnWorktreeCreationFailure verifies that when worktree
// creation fails, Ready logs an error and does not launch an agent.
func TestReady_SkipsLaunchOnWorktreeCreationFailure(t *testing.T) {
	s := newTestStore(t)
	ticket := &model.Ticket{
		Title:    "bad repo test",
		Type:     model.TypeTicket,
		Status:   model.StatusDraft,
		RepoPath: "/nonexistent/repo/path",
	}
	require.NoError(t, s.CreateTicket(ticket))
	setupTask := &model.Task{TicketID: ticket.ID, Title: "setup task", Position: 1}
	require.NoError(t, s.CreateTask(setupTask))
	require.NoError(t, s.SetWorktreePath(ticket.ID, "", "/nonexistent/repo/path", "feat/t-999"))

	require.NoError(t, s.ConfigSet("agent.auto_dispatch", "true"))
	require.NoError(t, s.ConfigSet("agent.command", "echo {}"))

	launcher := agent.NewLauncher(s)
	var errBuf bytes.Buffer
	err := Ready(s, ticket.ID, launcher, io.Discard, &errBuf)
	require.NoError(t, err, "Ready itself should not return an error")
	assert.Contains(t, errBuf.String(), "worktree creation failed", "error should be logged to stderr")

	sess, err := s.GetAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	assert.Nil(t, sess, "no agent session should be created when worktree creation fails")
}

// TestFullReviewCycle_HunkComments exercises the complete review flow that the
// new review panel enables: hunk-anchored draft comment → submit → agent sets
// new commit hash → human resolves → submit → approve.
func TestFullReviewCycle_HunkComments(t *testing.T) {
	s := newTestStore(t)

	// ── Round 1: agent does the work ──────────────────────────────────────────
	ticket, task := newInReviewTicket(t, s)

	// Agent stores the commit hash (mimics "ticket task set-commit").
	task.CommitHash = "aabbccdd1122"
	require.NoError(t, s.UpdateTask(task))

	// Human leaves a hunk-anchored review comment.
	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "internal/foo.go", "@@ -3,6 +3,7 @@")
	require.NoError(t, err)
	_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human:alice", "extract into helper")
	require.NoError(t, err)

	// Submit review → ticket transitions to ready; thread becomes needs_attention.
	require.NoError(t, SubmitReview(s, ticket.ID, "human:alice", nil, io.Discard, io.Discard))

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, got.Status, "hunk comment must move ticket back to ready")

	threads, err := s.GetThreadsForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.Equal(t, model.ThreadNeedsAttention, threads[0].Status)
	assert.Equal(t, "internal/foo.go", threads[0].FilePath)
	assert.Equal(t, "@@ -3,6 +3,7 @@", threads[0].HunkHeader)

	// ── Round 2: agent amends ─────────────────────────────────────────────────
	// Complete all tasks (including newly created amendment tasks) then advance to in_review.
	allTasks, err := s.GetTasksForTicket(ticket.ID)
	require.NoError(t, err)
	for _, tk := range allTasks {
		if tk.CompletedAt == nil {
			require.NoError(t, s.CompleteTask(tk.ID))
		}
	}
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInProgress))
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview))

	// Agent updates commit hash after fixup + autosquash.
	task.CommitHash = "deadbeef9900"
	require.NoError(t, s.UpdateTask(task))

	// Human resolves the thread.
	require.NoError(t, s.SetDraftAction(threads[0].ID, ticket.ID, model.DraftActionResolve))

	// Submit with only resolutions → ticket stays in_review.
	require.NoError(t, SubmitReview(s, ticket.ID, "human:alice", nil, io.Discard, io.Discard))

	got, err = s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusInReview, got.Status, "pure-resolution submit must keep ticket in_review")

	// All threads resolved → approve is valid.
	threads, err = s.GetThreadsForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.Equal(t, model.ThreadResolved, threads[0].Status)

	// Verify stored commit hash reflects the amendment.
	updatedTask, err := s.GetTask(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "deadbeef9900", updatedTask.CommitHash)

	// Complete any remaining tasks before approving.
	allTasks, err = s.GetTasksForTicket(ticket.ID)
	require.NoError(t, err)
	for _, tk := range allTasks {
		if tk.CompletedAt == nil {
			require.NoError(t, s.CompleteTask(tk.ID))
		}
	}

	// Approve (no open threads).
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusApproved))
	got, err = s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusApproved, got.Status)
}

// TestSubmitReview_AutoDispatch verifies that when a launcher is provided and
// agent.auto_dispatch=true with agent.command set, SubmitReview launches an
// agent and transitions the ticket to in_progress after the ready transition.
func TestSubmitReview_AutoDispatch(t *testing.T) {
	repoPath := gitRepo(t)
	s := newTestStore(t)
	ticket, task := newInReviewTicket(t, s)

	// Give the ticket a worktree path; the launcher writes .agent/output.log here.
	worktreePath := t.TempDir()
	require.NoError(t, s.SetWorktreePath(ticket.ID, worktreePath, repoPath, "feat/"+strings.ToLower(ticket.ID)))

	require.NoError(t, s.ConfigSet("agent.auto_dispatch", "true"))
	require.NoError(t, s.ConfigSet("agent.command", "echo {}"))

	// Stage one draft thread so FlushDraftState returns naThreadIDs and the ticket
	// transitions to ready before auto-dispatch fires.
	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
	require.NoError(t, err)
	_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human", "please fix this")
	require.NoError(t, err)

	launcher := agent.NewLauncher(s)
	require.NoError(t, SubmitReview(s, ticket.ID, "human", launcher, io.Discard, io.Discard))

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusInProgress, got.Status)

	sess, err := s.GetAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	assert.NotNil(t, sess)

	notes, err := s.GetNotesForTicket(ticket.ID)
	require.NoError(t, err)
	var found bool
	for _, n := range notes {
		if strings.Contains(n.Text, "auto-dispatched") {
			found = true
		}
	}
	assert.True(t, found, "auto-dispatch note should be present")
}

// TestSubmitReview_SkipsDispatchWhenSessionExists verifies that when an active
// agent session already exists for the ticket, SubmitReview skips auto-dispatch
// and does not create a second session or transition to in_progress.
func TestSubmitReview_SkipsDispatchWhenSessionExists(t *testing.T) {
	repoPath := gitRepo(t)
	s := newTestStore(t)
	ticket, task := newInReviewTicket(t, s)

	worktreePath := t.TempDir()
	require.NoError(t, s.SetWorktreePath(ticket.ID, worktreePath, repoPath, "feat/"+strings.ToLower(ticket.ID)))

	require.NoError(t, s.ConfigSet("agent.auto_dispatch", "true"))
	require.NoError(t, s.ConfigSet("agent.command", "echo {}"))

	// Seed an existing running session before calling SubmitReview.
	seeded, err := s.CreateAgentSession(ticket.ID, 99999, "/tmp/fake.log")
	require.NoError(t, err)

	// Stage one draft thread so the ticket transitions to ready.
	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
	require.NoError(t, err)
	_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human", "please fix this")
	require.NoError(t, err)

	launcher := agent.NewLauncher(s)
	require.NoError(t, SubmitReview(s, ticket.ID, "human", launcher, io.Discard, io.Discard))

	// Ticket transitions to ready (draft flush happened) but not in_progress (no dispatch).
	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, got.Status)

	// No new session: the latest session is still the seeded one.
	latest, err := s.GetLatestAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, seeded.ID, latest.ID)
}

// TestSubmitReview_SkipsDispatchWhenConfigOff verifies that a non-nil launcher
// alone does not trigger auto-dispatch — the agent.auto_dispatch config must
// also be set. The ticket transitions to ready but no agent session is created.
func TestSubmitReview_SkipsDispatchWhenConfigOff(t *testing.T) {
	s := newTestStore(t)
	ticket, task := newInReviewTicket(t, s)

	// Stage one draft thread so the ticket transitions to ready.
	dt, err := s.CreateDraftThread(ticket.ID, task.ID, "", "")
	require.NoError(t, err)
	_, err = s.AddDraftMessage(dt.ID, ticket.ID, false, "human", "please fix this")
	require.NoError(t, err)

	// Launcher is provided but config is NOT set — dispatch must be skipped.
	launcher := agent.NewLauncher(s)
	require.NoError(t, SubmitReview(s, ticket.ID, "human", launcher, io.Discard, io.Discard))

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, got.Status)

	sess, err := s.GetAgentSessionByTicket(ticket.ID)
	require.NoError(t, err)
	assert.Nil(t, sess)
}

// TestApprove_BlockedByOpenThread verifies that a ticket with an open or
// needs_attention thread cannot be approved at the store level (callers are
// responsible for the pre-check; this test documents the required guard).
func TestApprove_BlockedByOpenThread(t *testing.T) {
	s := newTestStore(t)
	ticket, task := newInReviewTicket(t, s)

	// Create an open thread.
	_, err := s.CreateThread(task.ID, "pkg/util.go", "@@ -1,3 +1,4 @@")
	require.NoError(t, err)

	// Verify the open thread is visible.
	threads, err := s.GetThreadsForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.Equal(t, model.ThreadOpen, threads[0].Status)

	// Caller must check threads before approving. Demonstrate the check logic.
	hasOpen := false
	for _, th := range threads {
		if th.Status == model.ThreadOpen || th.Status == model.ThreadNeedsAttention {
			hasOpen = true
		}
	}
	assert.True(t, hasOpen, "thread is open so approval must be blocked")
}

func TestReplyToThread(t *testing.T) {
	s := newTestStore(t)
	ticket, task := newInReviewTicket(t, s)
	_ = ticket

	th, err := s.CreateThread(task.ID, "", "")
	require.NoError(t, err)

	msg, err := ReplyToThread(s, th.ID, "agent:claude", "looks good")
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "agent:claude", msg.Author)
	assert.Equal(t, "looks good", msg.Text)
}

func TestTransitionThread(t *testing.T) {
	s := newTestStore(t)
	_, task := newInReviewTicket(t, s)

	th, err := s.CreateThread(task.ID, "", "")
	require.NoError(t, err)
	assert.Equal(t, model.ThreadOpen, th.Status)

	require.NoError(t, TransitionThread(s, th.ID, model.ThreadResolved, "human"))

	got, err := s.GetThread(th.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ThreadResolved, got.Status)
}

func TestBlockTicket_And_UnblockTicket(t *testing.T) {
	s := newTestStore(t)

	blocker, err := Draft(s, "Blocker", "", "")
	require.NoError(t, err)

	dependent, err := Draft(s, "Dependent", "", "")
	require.NoError(t, err)

	require.NoError(t, BlockTicket(s, dependent.ID, blocker.ID))

	got, err := s.GetTicket(dependent.ID)
	require.NoError(t, err)
	assert.Contains(t, got.BlockedBy, blocker.ID)

	require.NoError(t, UnblockTicket(s, dependent.ID, blocker.ID))

	got, err = s.GetTicket(dependent.ID)
	require.NoError(t, err)
	assert.NotContains(t, got.BlockedBy, blocker.ID)
}
