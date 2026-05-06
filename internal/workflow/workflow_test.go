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
