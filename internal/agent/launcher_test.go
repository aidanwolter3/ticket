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

var (
	fakeAgentBin string
	oscAgentBin  string
	csiAgentBin  string
	pwdAgentBin  string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agent_bins")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	buildBin := func(pkg, name string) string {
		bin := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", bin, pkg)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			panic("build " + name + ": " + err.Error())
		}
		return bin
	}

	fakeAgentBin = buildBin("./testdata/fake_agent", "fake_agent")
	oscAgentBin = buildBin("./testdata/osc_agent", "osc_agent")
	csiAgentBin = buildBin("./testdata/csi_agent", "csi_agent")
	pwdAgentBin = buildBin("./testdata/pwd_agent", "pwd_agent")

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

// pollState polls every 20ms until the active session for ticketID equals want,
// returning the final observed state.
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

// waitLines polls the subscriber channel until pred returns true or timeout elapses.
func waitLines(t *testing.T, ch <-chan []string, timeout time.Duration, pred func([]string) bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case lines, ok := <-ch:
			if !ok {
				return false
			}
			if pred(lines) {
				return true
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	return false
}

// TestBuildTicketPrompt verifies that BuildTicketPrompt produces the exact full
// context sent to a dispatched agent — substituting the ticket ID and worktree
// path into the dispatched skill template.
func TestBuildTicketPrompt(t *testing.T) {
	ticketID := "T-042"
	worktreePath := "/some/worktree/path"

	t.Run("with worktree path", func(t *testing.T) {
		want := strings.ReplaceAll(dispatchedSkill, "{{TICKET_ID}}", ticketID)
		want = strings.ReplaceAll(want, "{{WORKTREE_CONTEXT}}", " and are already inside the correct worktree at "+worktreePath)
		assert.Equal(t, want, BuildTicketPrompt(ticketID, worktreePath))
	})

	t.Run("no worktree path", func(t *testing.T) {
		want := strings.ReplaceAll(dispatchedSkill, "{{TICKET_ID}}", ticketID)
		want = strings.ReplaceAll(want, "{{WORKTREE_CONTEXT}}", "")
		assert.Equal(t, want, BuildTicketPrompt(ticketID, ""))
	})
}

// TestBuildPrompt verifies that BuildPrompt produces a bash -c invocation with a
// $TICKET_AGENT_PROMPT reference, and returns errors for invalid templates.
func TestBuildPrompt(t *testing.T) {
	t.Run("success cases", func(t *testing.T) {
		cases := []struct {
			name      string
			template  string
			wantShell string
		}{
			{"bare placeholder", "claude -p {}", `claude -p "$TICKET_AGENT_PROMPT"`},
			{"double-quoted placeholder", `claude -p "{}"`, `claude -p "$TICKET_AGENT_PROMPT"`},
			{"single-quoted placeholder", "claude -p '{}'", `claude -p "$TICKET_AGENT_PROMPT"`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				args, err := BuildPrompt(tc.template)
				require.NoError(t, err)
				require.Len(t, args, 3)
				assert.Equal(t, "/bin/bash", args[0])
				assert.Equal(t, "-c", args[1])
				assert.Equal(t, tc.wantShell, args[2])
			})
		}
	})

	t.Run("error: missing placeholder", func(t *testing.T) {
		_, err := BuildPrompt("claude -p somestaticprompt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no '{}' placeholder")
	})
}

// TestLaunchStateTransitions verifies:
//
//	Launch → running → waiting (after silence) → "" (terminated after exit)
func TestLaunchStateTransitions(t *testing.T) {
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

	sess, err := launcher.Launch(ticket.ID, worktreeDir, []string{fakeAgentBin})
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, model.AgentRunning, sess.State)

	got := pollState(t, s, ticket.ID, model.AgentWaiting, 3*time.Second)
	assert.Equal(t, model.AgentWaiting, got, "state should become waiting after silence timeout")

	got = pollState(t, s, ticket.ID, "", 5*time.Second)
	assert.Equal(t, model.AgentState(""), got, "session should be inactive after process exits")

	logPath := filepath.Join(worktreeDir, ".agent", "output.log")
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "fake_agent: starting")
	assert.Contains(t, string(data), "fake_agent: finishing")
}

// TestTerminateAll verifies that TerminateAll marks all active sessions terminated.
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

	sess1, _ := s.GetAgentSessionByTicket(t1.ID)
	sess2, _ := s.GetAgentSessionByTicket(t2.ID)
	require.NotNil(t, sess1)
	require.NotNil(t, sess2)

	require.NoError(t, launcher.TerminateAll())

	sess1, _ = s.GetAgentSessionByTicket(t1.ID)
	sess2, _ = s.GetAgentSessionByTicket(t2.ID)
	assert.Nil(t, sess1, "t1 session should be terminated")
	assert.Nil(t, sess2, "t2 session should be terminated")
}

// TestOSCAgentNoLeak verifies that an OSC window title containing ✳ (U+2733)
// does not leak "Claude Code" as visible rendered text (regression for sanitizeOSCC1).
// Without sanitizeOSCC1, the 0x9C continuation byte in ✳ is misinterpreted as C1
// STRING TERMINATOR, prematurely dispatching the OSC and leaking the remaining
// title text onto the screen.
func TestOSCAgentNoLeak(t *testing.T) {
	SilenceTimeout = 5 * time.Second

	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	launcher := NewLauncher(s)
	sess, err := launcher.Launch(ticket.ID, t.TempDir(), []string{oscAgentBin})
	require.NoError(t, err)

	follow, unsub := launcher.Subscribe(sess.ID)
	defer unsub()

	var allLines []string
	found := waitLines(t, follow, 5*time.Second, func(lines []string) bool {
		for _, l := range lines {
			allLines = append(allLines, l)
			if strings.Contains(l, "osc_agent: ready") {
				return true
			}
		}
		return false
	})
	require.True(t, found, "expected 'osc_agent: ready' in rendered output")

	// The leaked OSC title text must not appear in any rendered line.
	// When 0x9C in ✳ is treated as ST, " Claude Code" is emitted as cell text.
	for _, l := range allLines {
		if strings.Contains(l, "Claude Code") {
			t.Errorf("OSC C1 leak detected in rendered line: %q", l)
		}
	}
}

// TestLaunch_SetsWorkingDirectory verifies that when worktreePath is non-empty,
// the launched process has its working directory set to that path.
func TestLaunch_SetsWorkingDirectory(t *testing.T) {
	SilenceTimeout = 5 * time.Second

	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	worktreeDir := t.TempDir()
	launcher := NewLauncher(s)

	sess, err := launcher.Launch(ticket.ID, worktreeDir, []string{pwdAgentBin})
	require.NoError(t, err)

	follow, unsub := launcher.Subscribe(sess.ID)
	defer unsub()

	found := waitLines(t, follow, 3*time.Second, func(lines []string) bool {
		for _, l := range lines {
			if strings.Contains(l, worktreeDir) {
				return true
			}
		}
		return false
	})
	assert.True(t, found, "agent working directory should match worktreePath")
}

// TestCSIcNoDeadlock verifies that CSI c (device-attributes query) does not
// deadlock the emulator (regression for vtResponseLoop).
func TestCSIcNoDeadlock(t *testing.T) {
	SilenceTimeout = 5 * time.Second

	s := newTestStore(t)
	ticket := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
	require.NoError(t, s.CreateTicket(ticket))

	launcher := NewLauncher(s)
	sess, err := launcher.Launch(ticket.ID, t.TempDir(), []string{csiAgentBin})
	require.NoError(t, err)

	follow, unsub := launcher.Subscribe(sess.ID)
	defer unsub()

	found := waitLines(t, follow, 2*time.Second, func(lines []string) bool {
		for _, l := range lines {
			if strings.Contains(l, "csi_agent: done") {
				return true
			}
		}
		return false
	})
	assert.True(t, found, "'csi_agent: done' should appear within 2s (no deadlock)")
}
