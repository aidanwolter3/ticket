package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/aidanwolter/ticket/internal/bubbleterm/emulator"
	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

// SilenceTimeout is the duration of no PTY output before the session state
// transitions to "waiting". Exposed as a package-level var so tests can shorten it.
var SilenceTimeout = 5 * time.Second

// PTYCols and PTYRows are the fixed PTY dimensions used when launching agents.
const PTYCols = 220
const PTYRows = 50

// Launcher manages agent sessions for a store.
type Launcher struct {
	store     *store.Store
	mu        sync.Mutex
	emulators map[string]*emulator.Emulator  // sessionID → emulator
	followers map[string][]chan []string      // sessionID → live-output subscribers
}

func NewLauncher(s *store.Store) *Launcher {
	return &Launcher{
		store:     s,
		emulators: make(map[string]*emulator.Emulator),
		followers: make(map[string][]chan []string),
	}
}

// WriteToAgent sends input bytes to the agent's PTY (keyboard forwarding).
func (l *Launcher) WriteToAgent(sessionID string, data []byte) error {
	l.mu.Lock()
	em := l.emulators[sessionID]
	l.mu.Unlock()
	if em == nil {
		return fmt.Errorf("no active session: %s", sessionID)
	}
	_, err := em.Write(data)
	return err
}

// Subscribe registers a channel that receives rendered screen lines for sessionID.
// The returned cancel func must be called to unsubscribe; the channel is closed
// when the session ends. If the session is no longer active (already terminated),
// a pre-closed channel is returned so callers fall through to log-only view.
func (l *Launcher) Subscribe(sessionID string) (<-chan []string, func()) {
	l.mu.Lock()
	if _, active := l.emulators[sessionID]; !active {
		l.mu.Unlock()
		ch := make(chan []string)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan []string, 64)
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
		// Guard against double-close: runAgent may have already closed this
		// channel during session cleanup.
		defer func() { recover() }()
		close(ch)
	}
	return ch, cancel
}

// broadcast sends rendered lines to all live subscribers of sessionID.
func (l *Launcher) broadcast(sessionID string, lines []string) {
	l.mu.Lock()
	followers := l.followers[sessionID]
	l.mu.Unlock()

	cp := make([]string, len(lines))
	copy(cp, lines)
	for _, ch := range followers {
		broadcastSend(ch, cp)
	}
}

func broadcastSend(ch chan []string, lines []string) {
	defer func() { recover() }()
	select {
	case ch <- lines:
	default:
	}
}

// Launch forks args under a PTY via the bubbleterm emulator, streams raw output
// to {worktreePath}/.agent/output.log, and creates an agent_sessions row.
// The silence monitor goroutine transitions state running↔waiting based on output activity.
func (l *Launcher) Launch(ticketID, worktreePath string, args []string) (*model.AgentSession, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}

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

	em, err := emulator.New(PTYCols, PTYRows)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("create agent emulator: %w", err)
	}

	// gotOutput signals the silence monitor when new PTY bytes arrive.
	gotOutput := make(chan struct{}, 1)
	em.SetOnRawOutput(func(data []byte) {
		logFile.Write(data) //nolint:errcheck
		select {
		case gotOutput <- struct{}{}:
		default:
		}
	})

	// exitCh receives the process exit error from the emulator's exit callback.
	exitCh := make(chan error, 1)
	em.SetOnExit(func(_ string, exitErr error) {
		exitCh <- exitErr
	})

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "TICKET_AGENT_PROMPT="+workSkill)

	if err := em.StartCommand(cmd); err != nil {
		em.Close()
		logFile.Close()
		return nil, fmt.Errorf("start agent: %w", err)
	}

	sess, err := l.store.CreateAgentSession(ticketID, cmd.Process.Pid, logPath)
	if err != nil {
		em.Close()
		logFile.Close()
		cmd.Process.Kill()
		return nil, fmt.Errorf("create agent session: %w", err)
	}

	l.mu.Lock()
	l.emulators[sess.ID] = em
	l.mu.Unlock()

	go l.runAgent(sess.ID, em, logFile, gotOutput, exitCh)

	return sess, nil
}

// runAgent drives state transitions, broadcasts rendered frames, and cleans up
// when the process exits.
func (l *Launcher) runAgent(sessionID string, em *emulator.Emulator, logFile *os.File, gotOutput <-chan struct{}, exitCh <-chan error) {
	defer func() {
		logFile.Close()
		em.Close()
		l.mu.Lock()
		delete(l.emulators, sessionID)
		for _, ch := range l.followers[sessionID] {
			close(ch)
		}
		delete(l.followers, sessionID)
		l.mu.Unlock()
	}()

	// Silence monitor: transitions state running↔waiting.
	silenceTimer := time.NewTimer(SilenceTimeout)
	defer silenceTimer.Stop()

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

	// Broadcast loop: on each damage signal, get the rendered screen and broadcast.
	go func() {
		for range em.DamageChan() {
			frame := em.GetScreen()
			l.broadcast(sessionID, frame.Rows)
		}
	}()

	// Wait for process to exit.
	waitErr := <-exitCh

	// Determine final state from process exit.
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			if status, ok2 := exitErr.Sys().(syscall.WaitStatus); ok2 && status.Signaled() {
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
func (l *Launcher) TerminateAll() error {
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
