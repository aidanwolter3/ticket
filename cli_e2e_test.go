package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aidanwolter/ticket/internal/store"
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
