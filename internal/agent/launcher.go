package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

// SilenceTimeout is the duration of no PTY output before the session state
// transitions to "waiting". Exposed as a package-level var so tests can shorten it.
var SilenceTimeout = 5 * time.Second

// PTYCols and PTYRows are the fixed PTY dimensions used when launching agents.
// The attach view creates a vt10x terminal with these exact same dimensions so
// cursor-positioning sequences are interpreted correctly.
const PTYCols = 220
const PTYRows = 50

// Launcher manages agent sessions for a store.
type Launcher struct {
	store     *store.Store
	mu        sync.Mutex
	ptys      map[string]*os.File        // sessionID → PTY master fd
	followers map[string][]chan []byte    // sessionID → live-output subscribers
}

func NewLauncher(s *store.Store) *Launcher {
	return &Launcher{
		store:     s,
		ptys:      make(map[string]*os.File),
		followers: make(map[string][]chan []byte),
	}
}

// PTYMaster returns the PTY master file for the given session (for stdin forwarding).
func (l *Launcher) PTYMaster(sessionID string) *os.File {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ptys[sessionID]
}

// Subscribe registers a channel that receives live PTY output for sessionID.
// The returned cancel func must be called to unsubscribe; the channel is closed
// when the session ends. If the session is no longer active (already terminated),
// a pre-closed channel is returned so callers fall through to log-only view.
func (l *Launcher) Subscribe(sessionID string) (<-chan []byte, func()) {
	l.mu.Lock()
	if _, active := l.ptys[sessionID]; !active {
		// Session already gone — return a closed channel; no unsub needed.
		l.mu.Unlock()
		ch := make(chan []byte)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan []byte, 64)
	l.followers[sessionID] = append(l.followers[sessionID], ch)
	l.mu.Unlock()

	cancel := func() {
		l.mu.Lock()
		followers := l.followers[sessionID]
		for i, c := range followers {
			if c == ch {
				l.followers[sessionID] = append(followers[:i], followers[i+1:]...)
				break
			}
		}
		l.mu.Unlock()
		close(ch) // unblock any goroutine waiting on this channel
	}
	return ch, cancel
}

// broadcast sends a copy of data to all live subscribers of sessionID.
// Must NOT be called with l.mu held.
func (l *Launcher) broadcast(sessionID string, data []byte) {
	l.mu.Lock()
	followers := l.followers[sessionID]
	l.mu.Unlock()

	cp := make([]byte, len(data))
	copy(cp, data)
	for _, ch := range followers {
		broadcastSend(ch, cp)
	}
}

// broadcastSend sends data to ch without blocking. If ch was concurrently closed
// (e.g. by Subscribe's cancel func), the recovered panic is silently discarded.
func broadcastSend(ch chan []byte, data []byte) {
	defer func() { recover() }()
	select {
	case ch <- data:
	default:
	}
}

// Launch forks args under a PTY, streams output to {worktreePath}/.agent/output.log,
// and creates an agent_sessions row. The silence monitor goroutine transitions
// state running↔waiting based on output activity.
func (l *Launcher) Launch(ticketID, worktreePath string, args []string) (*model.AgentSession, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	// Use a temp dir for the log when the ticket has no worktree yet (ready state);
	// the agent will create its own worktree after claiming work.
	logBase := worktreePath
	if logBase == "" {
		logBase = filepath.Join(os.TempDir(), "ticket-agent-"+ticketID)
	}
	logDir := filepath.Join(logBase, ".agent")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("create agent log dir: %w", err)
	}
	logPath := filepath.Join(logDir, "output.log")

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open agent log: %w", err)
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "TICKET_AGENT_PROMPT="+workSkill)

	ptym, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: PTYRows, Cols: PTYCols})
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start agent pty: %w", err)
	}

	sess, err := l.store.CreateAgentSession(ticketID, cmd.Process.Pid, logPath)
	if err != nil {
		ptym.Close()
		logFile.Close()
		cmd.Process.Kill()
		return nil, fmt.Errorf("create agent session: %w", err)
	}

	l.mu.Lock()
	l.ptys[sess.ID] = ptym
	l.mu.Unlock()

	go l.runAgent(sess.ID, cmd, ptym, logFile)

	return sess, nil
}

// runAgent reads PTY output, writes to logFile, broadcasts to subscribers,
// drives state transitions, and cleans up when the process exits.
func (l *Launcher) runAgent(sessionID string, cmd *exec.Cmd, ptym *os.File, logFile *os.File) {
	defer func() {
		logFile.Close()
		l.mu.Lock()
		delete(l.ptys, sessionID)
		// Close all subscriber channels so attached viewers know the session ended.
		for _, ch := range l.followers[sessionID] {
			close(ch)
		}
		delete(l.followers, sessionID)
		l.mu.Unlock()
	}()

	// Wait for process exit in a goroutine; close ptym to unblock ptym.Read.
	exitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		ptym.Close()
		exitCh <- err
	}()

	// Silence monitor: transitions state running↔waiting.
	silenceTimer := time.NewTimer(SilenceTimeout)
	defer silenceTimer.Stop()
	gotOutput := make(chan struct{}, 1)

	go func() {
		for {
			select {
			case <-silenceTimer.C:
				l.store.UpdateAgentSessionState(sessionID, model.AgentWaiting)
			case _, ok := <-gotOutput:
				if !ok {
					return
				}
				l.store.UpdateAgentSessionState(sessionID, model.AgentRunning)
				if !silenceTimer.Stop() {
					select {
					case <-silenceTimer.C:
					default:
					}
				}
				silenceTimer.Reset(SilenceTimeout)
			}
		}
	}()

	// Read PTY output, write to log, and broadcast to any attached viewers.
	buf := make([]byte, 4096)
	for {
		n, err := ptym.Read(buf)
		if n > 0 {
			logFile.Write(buf[:n])
			l.broadcast(sessionID, buf[:n])
			select {
			case gotOutput <- struct{}{}:
			default:
			}
		}
		if err != nil {
			break
		}
	}

	close(gotOutput)

	// Determine final state from process exit code.
	waitErr := <-exitCh
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			if status, ok2 := exitErr.Sys().(syscall.WaitStatus); ok2 && status.Signaled() {
				// Killed by signal (e.g. SIGTERM from Terminate) — treat as terminated.
				l.store.UpdateAgentSessionState(sessionID, model.AgentTerminated)
			} else {
				l.store.UpdateAgentSessionState(sessionID, model.AgentCrashed)
			}
		} else {
			l.store.UpdateAgentSessionState(sessionID, model.AgentCrashed)
		}
	} else {
		l.store.UpdateAgentSessionState(sessionID, model.AgentTerminated)
	}
}

// Terminate sends SIGTERM to the agent process and marks the session terminated.
func (l *Launcher) Terminate(ticketID string) error {
	sess, err := l.store.GetAgentSessionByTicket(ticketID)
	if err != nil {
		return err
	}
	if sess == nil {
		return nil
	}

	proc, err := os.FindProcess(sess.PID)
	if err == nil {
		proc.Signal(syscall.SIGTERM)
	}

	return l.store.UpdateAgentSessionState(sess.ID, model.AgentTerminated)
}

// TerminateAll terminates all active agent sessions. Called on TUI shutdown.
// It sends SIGTERM to each known process and marks all active sessions terminated.
func (l *Launcher) TerminateAll() error {
	l.mu.Lock()
	ptys := make(map[string]*os.File, len(l.ptys))
	for k, v := range l.ptys {
		ptys[k] = v
	}
	l.mu.Unlock()

	// Send SIGTERM to all known active processes via the store.
	sessions, err := l.store.ListActiveAgentSessions()
	if err == nil {
		for _, sess := range sessions {
			proc, findErr := os.FindProcess(sess.PID)
			if findErr == nil {
				proc.Signal(syscall.SIGTERM)
			}
		}
	}

	return l.store.TerminateAllAgentSessions()
}
