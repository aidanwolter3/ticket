package integration_test

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// run executes globalTicketBin with args, capturing stdout and stderr separately.
// It returns (stdout, stderr, exitCode) without calling t.Fatal on non-zero exit —
// callers assert the exit code themselves.
func run(t *testing.T, dbPath string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	_ = dbPath // dbPath is passed for readability; callers include --db in args
	cmd := exec.Command(globalTicketBin, args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// newTestDB opens a fresh SQLite store in a temp directory. Returns the file
// path (for passing as --db to the CLI) and the open store (for seeding state).
func newTestDB(t *testing.T) (string, *store.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Skipf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return dbPath, s
}

// decodeJSON unmarshals raw JSON into a map for field inspection.
func decodeJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &out), "decode JSON")
	return out
}

// cliGitRepo creates a temp directory with an initialized git repo containing
// one commit on 'main'. Returns the repo path.
func cliGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git("add", ".")
	git("commit", "-m", "initial")
	return dir
}

func TestCLI_CRUD(t *testing.T) {
	if globalBuildFailed || globalTicketBin == "" {
		t.Skip("ticket binary could not be built")
	}

	db, _ := newTestDB(t)
	repoPath := t.TempDir()

	// draft
	stdout, _, code := run(t, db, "draft", "--db", db, "--title", "First Ticket", "--repo", repoPath)
	require.Equal(t, 0, code, "draft should exit 0")
	ticketID := strings.TrimSpace(stdout)
	require.NotEmpty(t, ticketID)

	// ls
	stdout, _, code = run(t, db, "ls", "--db", db)
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, ticketID)

	// ls --status filter
	stdout, _, code = run(t, db, "ls", "--db", db, "--status", "draft")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, ticketID)

	stdout, _, code = run(t, db, "ls", "--db", db, "--status", "ready")
	require.Equal(t, 0, code)
	assert.NotContains(t, stdout, ticketID)

	// get plain text
	stdout, _, code = run(t, db, "get", "--db", db, ticketID)
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, ticketID)
	assert.Contains(t, stdout, "First Ticket")

	// get --json round-trip
	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj := decodeJSON(t, stdout)
	assert.Equal(t, ticketID, tj["id"])
	assert.Equal(t, "First Ticket", tj["title"])
	assert.Equal(t, "draft", tj["status"])

	// task add
	stdout, _, code = run(t, db, "task", "add", ticketID, "--db", db, "--title", "Do the work")
	require.Equal(t, 0, code)
	taskID := strings.TrimSpace(stdout)
	require.NotEmpty(t, taskID)

	// task ls
	stdout, _, code = run(t, db, "task", "ls", "--db", db, ticketID)
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, taskID)

	// task ls --json
	stdout, _, code = run(t, db, "task", "ls", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	var tasks []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &tasks))
	require.Len(t, tasks, 1)
	assert.Equal(t, taskID, tasks[0]["id"])
	assert.Equal(t, "Do the work", tasks[0]["title"])

	// task update
	stdout, _, code = run(t, db, "task", "update", taskID, "--db", db, "--title", "Updated task")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, taskID)

	stdout, _, code = run(t, db, "task", "ls", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	require.NoError(t, json.Unmarshal([]byte(stdout), &tasks))
	assert.Equal(t, "Updated task", tasks[0]["title"])

	// task move (position 1 → 1, no-op, should succeed)
	_, _, code = run(t, db, "task", "move", taskID, "--db", db, "1")
	require.Equal(t, 0, code)

	// task add extra + task delete
	stdout, _, code = run(t, db, "task", "add", ticketID, "--db", db, "--title", "Extra task")
	require.Equal(t, 0, code)
	extraTaskID := strings.TrimSpace(stdout)

	_, _, code = run(t, db, "task", "delete", "--db", db, extraTaskID)
	require.Equal(t, 0, code)

	stdout, _, code = run(t, db, "task", "ls", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	require.NoError(t, json.Unmarshal([]byte(stdout), &tasks))
	require.Len(t, tasks, 1, "extra task should be deleted")

	// update ticket title
	stdout, _, code = run(t, db, "update", ticketID, "--db", db, "--title", "Updated Title")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, ticketID)

	stdout, _, code = run(t, db, "get", "--db", db, ticketID)
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Updated Title")

	// note add reflected in get --json
	_, _, code = run(t, db, "note", "add", "--db", db, ticketID, "agent:test", "test note text")
	require.Equal(t, 0, code)

	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj = decodeJSON(t, stdout)
	notes, _ := tj["notes"].([]any)
	require.NotEmpty(t, notes)
	firstNote := notes[0].(map[string]any)
	assert.Equal(t, "test note text", firstNote["text"])

	// block / unblock reflected in blocked_by
	stdout, _, code = run(t, db, "draft", "--db", db, "--title", "Blocker", "--repo", repoPath)
	require.Equal(t, 0, code)
	blockerID := strings.TrimSpace(stdout)

	_, _, code = run(t, db, "block", "--db", db, ticketID, blockerID)
	require.Equal(t, 0, code)

	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj = decodeJSON(t, stdout)
	blockedBy, _ := tj["blocked_by"].([]any)
	require.Contains(t, blockedBy, blockerID)

	_, _, code = run(t, db, "unblock", "--db", db, ticketID, blockerID)
	require.Equal(t, 0, code)

	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj = decodeJSON(t, stdout)
	blockedBy, _ = tj["blocked_by"].([]any)
	assert.Empty(t, blockedBy)

	// config set / get roundtrip
	_, _, code = run(t, db, "config", "set", "--db", db, "worktrees", "false")
	require.Equal(t, 0, code)

	stdout, _, code = run(t, db, "config", "get", "--db", db, "worktrees")
	require.Equal(t, 0, code)
	assert.Equal(t, "false\n", stdout)

	// import from JSON file → tickets appear in ls
	importJSON := `{"tickets": [{"title": "Imported Ticket", "repo_path": "` + repoPath + `"}]}`
	importFile := filepath.Join(t.TempDir(), "import.json")
	require.NoError(t, os.WriteFile(importFile, []byte(importJSON), 0644))

	stdout, _, code = run(t, db, "import", "--db", db, importFile)
	require.Equal(t, 0, code)

	stdout, _, code = run(t, db, "ls", "--db", db)
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Imported Ticket")

	// delete (draft only) — ticketID is still draft
	_, _, code = run(t, db, "delete", "--db", db, ticketID)
	require.Equal(t, 0, code)

	stdout, _, code = run(t, db, "ls", "--db", db)
	require.Equal(t, 0, code)
	assert.NotContains(t, stdout, ticketID)

	// delete enforcement: non-draft ticket should fail
	stdout, _, code = run(t, db, "draft", "--db", db, "--title", "Non-deletable", "--repo", repoPath)
	require.Equal(t, 0, code)
	ndID := strings.TrimSpace(stdout)
	_, _, code = run(t, db, "task", "add", ndID, "--db", db, "--title", "task")
	require.Equal(t, 0, code)
	_, _, code = run(t, db, "ready", "--db", db, ndID)
	require.Equal(t, 0, code)
	_, _, code = run(t, db, "delete", "--db", db, ndID)
	assert.NotEqual(t, 0, code, "delete of a ready ticket should fail")
}

func TestCLI_AgentCommands(t *testing.T) {
	if globalBuildFailed || globalTicketBin == "" {
		t.Skip("ticket binary could not be built")
	}

	db, s := newTestDB(t)
	repoPath := cliGitRepo(t)

	// Seed via store: ticket in ready state with two tasks.
	ticket := &model.Ticket{
		Title:    "Agent Test",
		Type:     model.TypeTicket,
		Status:   model.StatusDraft,
		RepoPath: repoPath,
	}
	require.NoError(t, s.CreateTicket(ticket))
	ticketID := ticket.ID

	task1 := &model.Task{TicketID: ticketID, Title: "Task one", Position: 1}
	require.NoError(t, s.CreateTask(task1))
	task1ID := task1.ID

	task2 := &model.Task{TicketID: ticketID, Title: "Task two", Position: 2}
	require.NoError(t, s.CreateTask(task2))
	task2ID := task2.ID

	require.NoError(t, s.TransitionTicket(ticketID, model.StatusReady))

	// --agent in-progress
	stdout, _, code := run(t, db, "--agent", "in-progress", "--db", db, ticketID)
	require.Equal(t, 0, code, "stdout=%q", stdout)

	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj := decodeJSON(t, stdout)
	assert.Equal(t, "in_progress", tj["status"])

	// --agent task complete --most-recent-commit task1
	stdout, _, code = run(t, db, "--agent", "task", "complete", "--db", db, "--most-recent-commit", task1ID)
	require.Equal(t, 0, code, "stdout=%q", stdout)

	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj = decodeJSON(t, stdout)
	require.NotNil(t, tj["tasks"])
	tasks := tj["tasks"].([]any)
	findTask := func(id string) map[string]any {
		for _, task := range tasks {
			td := task.(map[string]any)
			if td["id"].(string) == id {
				return td
			}
		}
		return nil
	}
	task1Data := findTask(task1ID)
	require.NotNil(t, task1Data)
	assert.NotNil(t, task1Data["completed_at"], "task1 should be completed")
	assert.NotEmpty(t, task1Data["commit_hash"], "task1 should have commit hash")

	// --agent task complete --commit <hash> task2
	headOut, err := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	commitHash := strings.TrimSpace(string(headOut))

	stdout, _, code = run(t, db, "--agent", "task", "complete", "--db", db, "--commit", commitHash, task2ID)
	require.Equal(t, 0, code, "stdout=%q", stdout)

	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj = decodeJSON(t, stdout)
	tasks = tj["tasks"].([]any)
	task2Data := findTask(task2ID)
	require.NotNil(t, task2Data)
	assert.NotNil(t, task2Data["completed_at"], "task2 should be completed")
	assert.Equal(t, commitHash, task2Data["commit_hash"])

	// --agent in-review (all tasks complete)
	stdout, _, code = run(t, db, "--agent", "in-review", "--db", db, ticketID)
	require.Equal(t, 0, code, "stdout=%q", stdout)

	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj = decodeJSON(t, stdout)
	assert.Equal(t, "in_review", tj["status"])
}

func TestCLI_FullLifecycle(t *testing.T) {
	if globalBuildFailed || globalTicketBin == "" || globalEchoAgent == "" {
		t.Skip("ticket or echo_agent binary could not be built")
	}

	db, s := newTestDB(t)
	repoPath := cliGitRepo(t)

	// Step 1: draft ticket.
	stdout, _, code := run(t, db, "draft", "--db", db, "--title", "Lifecycle Ticket", "--repo", repoPath)
	require.Equal(t, 0, code)
	ticketID := strings.TrimSpace(stdout)

	// Step 2: add task.
	stdout, _, code = run(t, db, "task", "add", ticketID, "--db", db, "--title", "Implement feature")
	require.Equal(t, 0, code)
	taskID := strings.TrimSpace(stdout)

	// Step 3: seed agent config via store.
	require.NoError(t, s.ConfigSet("agent.command", globalEchoAgent+" {}"))
	require.NoError(t, s.ConfigSet("agent.auto_dispatch", "true"))

	// Step 4: ticket ready → auto-dispatch creates worktree, launches echo_agent,
	// auto-transitions to in_progress.
	_, _, code = run(t, db, "ready", "--db", db, ticketID)
	require.Equal(t, 0, code)

	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj := decodeJSON(t, stdout)
	assert.Equal(t, "in_progress", tj["status"])
	worktreePath, _ := tj["worktree_path"].(string)
	require.NotEmpty(t, worktreePath, "worktree_path should be set after auto-dispatch")

	// Step 5: make a git commit in the worktree.
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v in %s: %s", args, dir, out)
	}
	git(worktreePath, "config", "user.email", "test@example.com")
	git(worktreePath, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(worktreePath, "feature.txt"), []byte("feature\n"), 0644))
	git(worktreePath, "add", ".")
	git(worktreePath, "commit", "-m", "add feature")

	// Step 6: complete task using --most-recent-commit (resolves HEAD from worktree_path).
	stdout, _, code = run(t, db, "--agent", "task", "complete", "--db", db, "--most-recent-commit", taskID)
	require.Equal(t, 0, code, "stdout=%q", stdout)

	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj = decodeJSON(t, stdout)
	require.NotNil(t, tj["tasks"])
	tasks := tj["tasks"].([]any)
	require.Len(t, tasks, 1)
	task1 := tasks[0].(map[string]any)
	assert.NotNil(t, task1["completed_at"], "task should be completed")
	assert.NotEmpty(t, task1["commit_hash"], "task should have commit hash")

	// Step 7: in-review.
	_, _, code = run(t, db, "--agent", "in-review", "--db", db, ticketID)
	require.Equal(t, 0, code)

	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj = decodeJSON(t, stdout)
	assert.Equal(t, "in_review", tj["status"])

	// Step 8: seed approved via store.
	require.NoError(t, s.TransitionTicket(ticketID, model.StatusApproved))

	// Step 9: merge directly.
	require.NoError(t, workflow.Merge(s, ticketID, io.Discard, io.Discard))

	// Step 10: verify merged and worktree directory gone.
	stdout, _, code = run(t, db, "get", "--db", db, "--json", ticketID)
	require.Equal(t, 0, code)
	tj = decodeJSON(t, stdout)
	assert.Equal(t, "merged", tj["status"])

	_, statErr := os.Stat(worktreePath)
	assert.True(t, os.IsNotExist(statErr), "worktree directory should not exist after merge")
}
