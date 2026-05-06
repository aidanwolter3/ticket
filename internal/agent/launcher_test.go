package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fakeAgentBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fake_agent_bin")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	bin := filepath.Join(dir, "fake_agent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/fake_agent")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("build fake_agent: " + err.Error())
	}
	fakeAgentBin = bin

	os.Exit(m.Run())
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

// pollState polls every 20ms until the active session for ticketID equals want
// (or is nil when want==""), returning the final observed state.
func pollState(t *testing.T, s *store.Store, ticketID string, want model.AgentState, timeout time.Duration) model.AgentState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sess, _ := s.GetAgentSessionByTicket(ticketID)
		state := model.AgentState("")
		if sess != nil {
			state = sess.State
		}
		if state == want {
			return want
		}
		time.Sleep(20 * time.Millisecond)
	}
	sess, _ := s.GetAgentSessionByTicket(ticketID)
	if sess == nil {
		return ""
	}
	return sess.State
}

// TestBuildPrompt verifies that BuildPrompt substitutes the embedded skill
// content correctly for various command template forms.
func TestBuildPrompt(t *testing.T) {
	cases := []struct {
		name      string
		template  string
		wantLen   int
		promptIdx int
	}{
		{"bare placeholder", "claude -p {}", 3, 2},
		{"double-quoted placeholder", `claude -p "{}"`, 3, 2},
		{"single-quoted placeholder", "claude -p '{}'", 3, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, err := BuildPrompt(tc.template)
			require.NoError(t, err)
			require.Len(t, args, tc.wantLen)
			assert.Equal(t, "claude", args[0])
			assert.Equal(t, "-p", args[1])
			prompt := args[tc.promptIdx]
			assert.NotEmpty(t, prompt, "substituted prompt must not be empty")
			assert.Contains(t, prompt, "claim-work", "prompt should contain work skill content")
			assert.False(t, strings.HasPrefix(prompt, "---"), "prompt must not start with YAML frontmatter")
		})
	}
}

// TestLaunchStateTransitions verifies:
//
//	Launch → running → waiting (after silence) → "" (terminated after exit)
//
// It also checks that output.log was written.
func TestLaunchStateTransitions(t *testing.T) {
	// Use a short silence timeout so the test runs quickly.
	SilenceTimeout = 80 * time.Millisecond

	s := newTestStore(t)
	ticket := &model.Ticket{
		Title:  "T",
		Type:   model.TypeTicket,
		Status: model.StatusDraft,
	}
	require.NoError(t, s.CreateTicket(ticket))

	worktreeDir := t.TempDir()
	launcher := NewLauncher(s)

	// Launch — initial state is running.
	sess, err := launcher.Launch(ticket.ID, worktreeDir, []string{fakeAgentBin})
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, model.AgentRunning, sess.State)

	// After 200ms silence (fake_agent pauses 200ms internally), state → waiting.
	// fake_agent pauses for 200ms; our SilenceTimeout is 80ms so it fires first.
	got := pollState(t, s, ticket.ID, model.AgentWaiting, 3*time.Second)
	assert.Equal(t, model.AgentWaiting, got, "state should become waiting after silence timeout")

	// After fake_agent finishes (it pauses 200ms total then exits), session → inactive.
	got = pollState(t, s, ticket.ID, "", 5*time.Second)
	assert.Equal(t, model.AgentState(""), got, "session should be inactive after process exits")

	// Verify output.log was written with expected content.
	logPath := filepath.Join(worktreeDir, ".agent", "output.log")
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "fake_agent: starting")
	assert.Contains(t, string(data), "fake_agent: finishing")
}

// TestTerminateAll verifies that TerminateAll sends SIGTERM to all active
// processes and marks their sessions terminated.
func TestTerminateAll(t *testing.T) {
	SilenceTimeout = 5 * time.Second

	s := newTestStore(t)

	t1 := &model.Ticket{Title: "T1", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(t1))
	t2 := &model.Ticket{Title: "T2", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(t2))

	dir1, dir2 := t.TempDir(), t.TempDir()
	launcher := NewLauncher(s)

	_, err := launcher.Launch(t1.ID, dir1, []string{fakeAgentBin})
	require.NoError(t, err)
	_, err = launcher.Launch(t2.ID, dir2, []string{fakeAgentBin})
	require.NoError(t, err)

	// Both sessions should be active.
	sess1, _ := s.GetAgentSessionByTicket(t1.ID)
	sess2, _ := s.GetAgentSessionByTicket(t2.ID)
	require.NotNil(t, sess1)
	require.NotNil(t, sess2)

	require.NoError(t, launcher.TerminateAll())

	// All sessions must be gone from active list.
	sess1, _ = s.GetAgentSessionByTicket(t1.ID)
	sess2, _ = s.GetAgentSessionByTicket(t2.ID)
	assert.Nil(t, sess1, "t1 session should be terminated")
	assert.Nil(t, sess2, "t2 session should be terminated")
}
