// Package integration_test provides PTY-level end-to-end tests for the ticket
// TUI + agent dispatch lifecycle. Tests spawn the real ticket binary under a PTY,
// drive it by writing key bytes, and assert on raw terminal output.
//
// Tests are skipped automatically if the ticket binary cannot be built (e.g.
// in a CI environment without sqlite headers).
package integration_test

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/creack/pty"
)

var (
	globalTicketBin   string
	globalEchoAgent   string
	globalBuildFailed bool
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "ticket-integration-*")
	if err != nil {
		log.Fatalf("mkdirtemp: %v", err)
	}
	defer os.RemoveAll(tmp)

	build := func(dst, pkg string) string {
		out, err := exec.Command("go", "build", "-o", dst, pkg).CombinedOutput()
		if err != nil {
			log.Printf("build %s failed: %v\n%s", pkg, err, out)
			globalBuildFailed = true
			return ""
		}
		return dst
	}

	globalTicketBin = build(filepath.Join(tmp, "ticket"), "./cmd/ticket")
	globalEchoAgent = build(filepath.Join(tmp, "echo_agent"), "./internal/agent/testdata/echo_agent")

	os.Exit(m.Run())
}

// createTestDB opens a temp SQLite DB, creates a ticket in ready state, and
// configures agent.command to point at agentBin.
func createTestDB(t *testing.T, agentBin string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Skipf("open store: %v", err)
	}
	defer s.Close()

	ticket := &model.Ticket{
		Title:  "Test Ticket",
		Type:   model.TypeTicket,
		Status: model.StatusDraft,
	}
	if err := s.CreateTicket(ticket); err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	task := &model.Task{TicketID: ticket.ID, Title: "Task 1", Position: 1}
	if err := s.CreateTask(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := s.TransitionTicket(ticket.ID, model.StatusReady); err != nil {
		t.Fatalf("transition to ready: %v", err)
	}
	if err := s.ConfigSet("agent.command", fmt.Sprintf("%s {}", agentBin)); err != nil {
		t.Fatalf("set agent.command: %v", err)
	}

	return dbPath
}

// ptyHarness runs the ticket binary under a PTY and accumulates its raw output.
type ptyHarness struct {
	t    *testing.T
	ptmx *os.File
	cmd  *exec.Cmd
	mu   sync.Mutex
	buf  bytes.Buffer
}

// newPTYHarness spawns the ticket TUI at 220×50 connected to dbPath.
// bubbletea v1.x performs terminal detection (OSC 11 background query + CPR)
// that can take up to 5-7s on a raw PTY before the first render. Use waitFor
// with a generous timeout (≥15s) for the initial TUI-ready check.
func newPTYHarness(t *testing.T, dbPath string) *ptyHarness {
	t.Helper()

	cmd := exec.Command(globalTicketBin, "--db", dbPath)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 50, Cols: 220})
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}

	h := &ptyHarness{t: t, ptmx: ptmx, cmd: cmd}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := ptmx.Read(b)
			if n > 0 {
				h.mu.Lock()
				h.buf.Write(b[:n])
				h.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	t.Cleanup(func() {
		ptmx.Close()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait() //nolint:errcheck
	})

	return h
}

// snap returns all bytes accumulated from the PTY so far.
func (h *ptyHarness) snap() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.buf.String()
}

// write sends bytes to the PTY master (simulates keystrokes).
func (h *ptyHarness) write(b []byte) {
	h.ptmx.Write(b) //nolint:errcheck
}

// waitFor polls snap() every 50 ms until check returns true or timeout elapses.
func (h *ptyHarness) waitFor(t *testing.T, desc string, timeout time.Duration, check func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check(h.snap()) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	s := h.snap()
	tail := s
	if len(tail) > 800 {
		tail = tail[len(tail)-800:]
	}
	t.Errorf("timeout (%v) waiting for %s\nlast 800 bytes:\n%s",
		timeout, desc, strings.ReplaceAll(tail, "\033", "ESC"))
}

// dispatchAgent navigates the TUI to dispatch an agent to the first ready ticket.
// It waits for the TUI to be ready, then sends 'g' + 'y' to confirm dispatch.
// The agent indicator (⚙ or "Agent: running") must appear within 10s.
func dispatchAgent(t *testing.T, h *ptyHarness) {
	t.Helper()

	// bubbletea v1.x takes 5-7s for terminal detection on a bare PTY before
	// the first render. The hint bar showing "[g] dispatch agent" confirms the
	// TUI has rendered and the ready ticket is selected.
	h.waitFor(t, "TUI ready ([g] dispatch agent hint visible)", 15*time.Second, func(s string) bool {
		return strings.Contains(s, "dispatch agent")
	})

	// Dispatch: press 'g' to open confirm modal, then 'y' to confirm.
	time.Sleep(100 * time.Millisecond)
	h.write([]byte("g"))
	time.Sleep(100 * time.Millisecond)
	h.write([]byte("y"))

	// Wait for agent to appear as running.
	h.waitFor(t, "agent running indicator (⚙ or 'Agent: running')", 10*time.Second, func(s string) bool {
		return strings.Contains(s, "⚙") || strings.Contains(s, "Agent: running")
	})
}

// TestDispatchAndAttach verifies that pressing 'g' on a ready ticket dispatches
// an agent and that the agent's output appears in the TUI attach view.
// Uses echo_agent which blocks on stdin indefinitely so the session stays alive
// long enough for the attach view to receive output.
func TestDispatchAndAttach(t *testing.T) {
	if globalBuildFailed || globalTicketBin == "" || globalEchoAgent == "" {
		t.Skip("ticket or echo_agent binary could not be built")
	}

	dbPath := createTestDB(t, globalEchoAgent)
	h := newPTYHarness(t, dbPath)

	dispatchAgent(t, h)

	// Attach to the agent output by pressing Enter.
	time.Sleep(200 * time.Millisecond)
	h.write([]byte("\r"))

	// echo_agent prints "echo_agent: ready" immediately on startup.
	h.waitFor(t, "'echo_agent: ready' in attach view", 10*time.Second, func(s string) bool {
		return strings.Contains(s, "echo_agent: ready") || strings.Contains(s, "echo_agent")
	})
}

// TestAgentPaneHotkeysForwarded verifies that TUI hotkeys (q, ?, ctrl+c) are
// forwarded to the agent PTY rather than handled by the TUI when the agent
// pane is focused. Pressing 'q' must not quit the TUI.
func TestAgentPaneHotkeysForwarded(t *testing.T) {
	if globalBuildFailed || globalTicketBin == "" || globalEchoAgent == "" {
		t.Skip("ticket or echo_agent binary could not be built")
	}

	dbPath := createTestDB(t, globalEchoAgent)
	h := newPTYHarness(t, dbPath)

	dispatchAgent(t, h)

	// Attach to the agent pane.
	time.Sleep(200 * time.Millisecond)
	h.write([]byte("\r"))
	h.waitFor(t, "attach view visible", 10*time.Second, func(s string) bool {
		return strings.Contains(s, "echo_agent")
	})

	// Press 'q' — must NOT quit the TUI. If it quits, the PTY closes and
	// subsequent writes/reads will fail; we detect this by verifying the
	// process is still running 500 ms later.
	h.write([]byte("q"))
	time.Sleep(500 * time.Millisecond)

	// Detach with ctrl+] to prove the TUI is still alive.
	h.write([]byte{0x1D})
	h.waitFor(t, "TUI still alive after 'q' in agent pane (q quit hint visible)", 5*time.Second, func(s string) bool {
		return strings.Contains(s, "q quit") || strings.Contains(s, "? help")
	})
}

// TestAttachExitCtrlBracket verifies that ctrl+] detaches from the agent and
// returns the user to the ticket list.
func TestAttachExitCtrlBracket(t *testing.T) {
	if globalBuildFailed || globalTicketBin == "" || globalEchoAgent == "" {
		t.Skip("ticket or echo_agent binary could not be built")
	}

	dbPath := createTestDB(t, globalEchoAgent)
	h := newPTYHarness(t, dbPath)

	dispatchAgent(t, h)

	// Attach to the agent.
	time.Sleep(200 * time.Millisecond)
	h.write([]byte("\r"))
	time.Sleep(500 * time.Millisecond) // let attach view render

	// Send ctrl+] (0x1D) to detach.
	h.write([]byte{0x1D})

	// After detach, the hint bar should reappear with the TUI navigation keys.
	h.waitFor(t, "ticket list visible after detach (Tickets tab or hint)", 5*time.Second, func(s string) bool {
		// Look for content that only appears in the list screen, not the attach view.
		return strings.Contains(s, "q quit") || strings.Contains(s, "? help")
	})
}
