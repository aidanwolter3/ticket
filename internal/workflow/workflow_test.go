package workflow

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
// repo/branch/worktree fields. No tasks are created so the task-complete check
// in Merge is vacuously satisfied.
func approvedTicket(t *testing.T, s *store.Store, repoPath, featureBranch, worktreePath string) *model.Ticket {
	t.Helper()
	ticket := &model.Ticket{
		Title:         "test",
		Type:          model.TypeTicket,
		Status:        model.StatusDraft,
		RepoPath:      repoPath,
		FeatureBranch: featureBranch,
		WorktreePath:  worktreePath,
	}
	require.NoError(t, s.CreateTicket(ticket))
	require.NoError(t, s.SetWorktreePath(ticket.ID, worktreePath, repoPath, featureBranch))
	for _, to := range []model.Status{
		model.StatusReady, model.StatusInProgress, model.StatusInReview, model.StatusApproved,
	} {
		author := "agent:claude"
		if to == model.StatusReady || to == model.StatusApproved {
			author = "human:test"
		}
		require.NoError(t, s.TransitionTicket(ticket.ID, to, author))
	}
	return ticket
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

func TestClaim_CreatesWorktree(t *testing.T) {
	s := newTestStore(t)
	repoPath := gitRepo(t)

	ticket := &model.Ticket{
		Title:    "Test ticket",
		Type:     model.TypeTicket,
		Status:   model.StatusReady,
		RepoPath: repoPath,
	}
	require.NoError(t, s.CreateTicket(ticket))

	item, err := Claim(s, "agent:test", io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, item)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, got.WorktreePath, "worktree_path should be set")
	assert.NotEmpty(t, got.FeatureBranch, "feature_branch should be set")
	_, statErr := os.Stat(got.WorktreePath)
	assert.NoError(t, statErr, "worktree directory should exist on disk")
}

func TestClaim_WorktreesDisabled(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.ConfigSet("worktrees", "false"))

	ticket := &model.Ticket{
		Title:  "Test ticket",
		Type:   model.TypeTicket,
		Status: model.StatusReady,
	}
	require.NoError(t, s.CreateTicket(ticket))

	item, err := Claim(s, "agent:test", io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, item)

	got, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Empty(t, got.WorktreePath)
	assert.Empty(t, got.FeatureBranch)
}

// TestWorktreeLifecycle exercises the full promote→claim→dependency-gate→requeue
// sequence end-to-end through the workflow and store layers.
func TestWorktreeLifecycle(t *testing.T) {
	s := newTestStore(t)
	repoPath := gitRepo(t)

	blocker := &model.Ticket{Title: "Blocker", Type: model.TypeTicket, Status: model.StatusDraft, RepoPath: repoPath}
	dependent := &model.Ticket{Title: "Dependent", Type: model.TypeTicket, Status: model.StatusDraft, RepoPath: repoPath}
	require.NoError(t, s.CreateTicket(blocker))
	require.NoError(t, s.CreateTicket(dependent))
	require.NoError(t, s.AddBlocker(dependent.ID, blocker.ID))

	// --- promote: no worktrees should be created ---
	require.NoError(t, Promote(s, blocker.ID, io.Discard, io.Discard))
	require.NoError(t, Promote(s, dependent.ID, io.Discard, io.Discard))

	b, err := s.GetTicket(blocker.ID)
	require.NoError(t, err)
	assert.Empty(t, b.WorktreePath, "promote must not create a worktree")
	assert.Empty(t, b.FeatureBranch, "promote must not set feature_branch")

	// --- claim blocker: worktree created ---
	item, err := Claim(s, "agent:test", io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, blocker.ID, item.Ticket.ID)

	b, err = s.GetTicket(blocker.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, b.WorktreePath)
	assert.NotEmpty(t, b.FeatureBranch)
	_, statErr := os.Stat(b.WorktreePath)
	require.NoError(t, statErr, "blocker worktree must exist on disk")
	blockerWorktree := b.WorktreePath

	// --- approved blocker does NOT unblock dependent ---
	require.NoError(t, s.TransitionTicket(blocker.ID, model.StatusInReview, "agent:test"))
	require.NoError(t, s.TransitionTicket(blocker.ID, model.StatusApproved, "human:test"))

	peek, err := s.PeekWork()
	require.NoError(t, err)
	for _, wi := range peek {
		assert.NotEqual(t, dependent.ID, wi.Ticket.ID, "dependent must not be claimable while blocker is only approved")
	}

	// --- merged blocker unblocks dependent ---
	require.NoError(t, s.TransitionTicket(blocker.ID, model.StatusMerged, "human:test"))

	peek, err = s.PeekWork()
	require.NoError(t, err)
	var found bool
	for _, wi := range peek {
		if wi.Ticket.ID == dependent.ID {
			found = true
		}
	}
	assert.True(t, found, "dependent must be claimable once blocker is merged")

	// --- claim dependent: gets its own fresh worktree ---
	item2, err := Claim(s, "agent:test", io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, item2)
	assert.Equal(t, dependent.ID, item2.Ticket.ID)

	d, err := s.GetTicket(dependent.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, d.WorktreePath)
	assert.NotEqual(t, blockerWorktree, d.WorktreePath, "dependent must get its own worktree")
	_, statErr = os.Stat(d.WorktreePath)
	require.NoError(t, statErr, "dependent worktree must exist on disk")
	dependentWorktree := d.WorktreePath

	// --- requeue (in_review → ready): worktree and branch survive ---
	require.NoError(t, s.TransitionTicket(dependent.ID, model.StatusInReview, "agent:test"))
	require.NoError(t, Ready(s, dependent.ID, "human:reviewer", io.Discard, io.Discard))

	d, err = s.GetTicket(dependent.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, d.Status)
	assert.NotEmpty(t, d.WorktreePath, "worktree_path must survive requeue")
	assert.NotEmpty(t, d.FeatureBranch, "feature_branch must survive requeue")
	_, statErr = os.Stat(dependentWorktree)
	assert.NoError(t, statErr, "worktree directory must still exist after requeue")
}

// TestClaim_AmendmentSkipsWorktreeCreation verifies that claiming amendment work
// returns the existing worktree_path unchanged and does not create a duplicate.
// TestReviewCycleLifecycle exercises the complete work→review→work→review→merge loop.
func TestReviewCycleLifecycle(t *testing.T) {
	s := newTestStore(t)
	repoPath := gitRepo(t)
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}

	// --- 1. Draft → ready → Claim (new work) → worktree created, in_progress ---
	ticket := &model.Ticket{
		Title:    "Review cycle ticket",
		Type:     model.TypeTicket,
		Status:   model.StatusDraft,
		RepoPath: repoPath,
	}
	require.NoError(t, s.CreateTicket(ticket))
	require.NoError(t, Promote(s, ticket.ID, io.Discard, io.Discard))

	item, err := Claim(s, "agent:test", io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, store.WorkTypeNew, item.Type)
	assert.Equal(t, ticket.ID, item.Ticket.ID)

	claimed, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusInProgress, claimed.Status)
	require.NotEmpty(t, claimed.WorktreePath, "worktree must be created on first claim")
	originalWorktree := claimed.WorktreePath
	originalBranch := claimed.FeatureBranch
	_, statErr := os.Stat(originalWorktree)
	require.NoError(t, statErr, "worktree directory must exist on disk")

	// Create a task and mark it complete (required for Merge).
	task := &model.Task{TicketID: ticket.ID, Title: "do the work", Position: 1}
	require.NoError(t, s.CreateTask(task))
	require.NoError(t, s.CompleteTask(task.ID))

	// Add a commit on the feature branch.
	git(originalWorktree, "config", "user.email", "test@example.com")
	git(originalWorktree, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(originalWorktree, "feature.txt"), []byte("feature\n"), 0644))
	git(originalWorktree, "add", ".")
	git(originalWorktree, "commit", "-m", ticket.ID+" "+task.ID+": do the work")

	// --- 2. Transition to in_review ---
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview, "agent:test"))

	// --- 3. Open a thread on the task, add a message ---
	th, err := s.CreateThread(task.ID)
	require.NoError(t, err)
	_, err = s.AddMessage(th.ID, "human:reviewer", "please rename this file")
	require.NoError(t, err)

	// --- 4. ReviewSubmit → ticket back to ready, thread now ready, worktree still on disk ---
	require.NoError(t, ReviewSubmit(s, ticket.ID, "human:reviewer", io.Discard, io.Discard))

	afterSubmit, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, afterSubmit.Status)
	assert.Equal(t, originalWorktree, afterSubmit.WorktreePath, "worktree must survive ReviewSubmit")
	assert.Equal(t, originalBranch, afterSubmit.FeatureBranch, "feature_branch must survive ReviewSubmit")
	_, statErr = os.Stat(originalWorktree)
	assert.NoError(t, statErr, "worktree directory must still exist on disk after ReviewSubmit")

	threads, err := s.GetThreadsForTicket(ticket.ID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.Equal(t, model.ThreadNeedsAttention, threads[0].Status, "thread must be needs_attention after ReviewSubmit")

	// --- 5. Claim (amendment work) — worktree_path unchanged, same branch, WorkTypeAmendment ---
	item2, err := Claim(s, "agent:test", io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, item2)
	assert.Equal(t, store.WorkTypeAmendment, item2.Type)
	assert.Equal(t, ticket.ID, item2.Ticket.ID)
	assert.Equal(t, originalWorktree, item2.Ticket.WorktreePath, "worktree_path must be unchanged for amendment")
	assert.Equal(t, originalBranch, item2.Ticket.FeatureBranch, "feature_branch must be unchanged for amendment")
	_, statErr = os.Stat(originalWorktree)
	assert.NoError(t, statErr, "original worktree must still exist")

	// --- 6. Reply to thread, transition thread ready → active ---
	_, err = s.AddMessage(th.ID, "agent:test", "renamed the file")
	require.NoError(t, err)
	require.NoError(t, s.TransitionThread(th.ID, model.ThreadOpen, "agent:test"))

	// Reviewer resolves the thread before approving.
	require.NoError(t, s.TransitionThread(th.ID, model.ThreadResolved, "human:reviewer"))

	// --- 7. Transition ticket back to in_review ---
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview, "agent:test"))

	// --- 8. Approve → Merge — worktree removed, branch deleted, ticket merged ---
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusApproved, "human:reviewer"))

	err = Merge(s, ticket.ID, io.Discard, io.Discard)
	require.NoError(t, err)

	merged, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusMerged, merged.Status)
	assert.Empty(t, merged.WorktreePath, "worktree_path must be cleared after merge")
	assert.Empty(t, merged.FeatureBranch, "feature_branch must be cleared after merge")
	_, statErr = os.Stat(originalWorktree)
	assert.True(t, os.IsNotExist(statErr), "worktree directory must be removed after merge")

	// Feature branch must be deleted.
	checkBranch := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", originalBranch)
	assert.Error(t, checkBranch.Run(), "feature branch must be deleted after merge")
}

func TestClaim_AmendmentSkipsWorktreeCreation(t *testing.T) {
	s := newTestStore(t)
	repoPath := gitRepo(t)

	ticket := &model.Ticket{
		Title:    "Amendment ticket",
		Type:     model.TypeTicket,
		Status:   model.StatusReady,
		RepoPath: repoPath,
	}
	require.NoError(t, s.CreateTicket(ticket))

	// Initial claim creates the worktree.
	item, err := Claim(s, "agent:test", io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, store.WorkTypeNew, item.Type)

	claimed, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	originalWorktree := claimed.WorktreePath
	require.NotEmpty(t, originalWorktree)

	// Advance to in_review, then back to ready via ReviewSubmit (simulating review cycle).
	// We need a task + thread for ReviewSubmit to work.
	task := &model.Task{TicketID: ticket.ID, Title: "task", Position: 1}
	require.NoError(t, s.CreateTask(task))
	th, err := s.CreateThread(task.ID)
	require.NoError(t, err)
	_, err = s.AddMessage(th.ID, "human:reviewer", "please fix this")
	require.NoError(t, err)

	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview, "agent:test"))
	require.NoError(t, ReviewSubmit(s, ticket.ID, "human:reviewer", io.Discard, io.Discard))

	// Ticket is now ready with a feature_branch — should be claimable as amendment.
	item2, err := Claim(s, "agent:test", io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, item2)
	assert.Equal(t, store.WorkTypeAmendment, item2.Type)
	assert.Equal(t, ticket.ID, item2.Ticket.ID)
	assert.Equal(t, originalWorktree, item2.Ticket.WorktreePath, "worktree_path must be unchanged for amendment")

	// The original worktree directory must still exist (not removed, not duplicated).
	_, statErr := os.Stat(originalWorktree)
	assert.NoError(t, statErr, "original worktree must still exist")
}

func TestReady_PreservesWorktreeFromInReview(t *testing.T) {
	s := newTestStore(t)
	repoPath := gitRepo(t)

	ticket := &model.Ticket{
		Title:    "Test ticket",
		Type:     model.TypeTicket,
		Status:   model.StatusReady,
		RepoPath: repoPath,
	}
	require.NoError(t, s.CreateTicket(ticket))

	// Claim to create the worktree.
	item, err := Claim(s, "agent:test", io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, item)

	// Advance to in_review.
	require.NoError(t, s.TransitionTicket(ticket.ID, model.StatusInReview, "agent:test"))

	claimed, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	worktreeDir := claimed.WorktreePath
	require.NotEmpty(t, worktreeDir)
	_, err = os.Stat(worktreeDir)
	require.NoError(t, err, "worktree should exist before requeue")

	// Requeue — worktree and branch should survive.
	require.NoError(t, Ready(s, ticket.ID, "human:reviewer", io.Discard, io.Discard))

	requeued, err := s.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.StatusReady, requeued.Status)
	assert.NotEmpty(t, requeued.WorktreePath, "worktree_path should survive requeue")
	assert.NotEmpty(t, requeued.FeatureBranch, "feature_branch should survive requeue")
	_, statErr := os.Stat(worktreeDir)
	assert.NoError(t, statErr, "worktree directory should still exist after requeue")
}
