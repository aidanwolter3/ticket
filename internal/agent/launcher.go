package agent

import (
	"fmt"
	"io"
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

// Launcher manages agent sessions for a store.
type Launcher struct {
	store *store.Store
	mu    sync.Mutex
	ptys  map[string]*os.File // sessionID → PTY master fd
}

func NewLauncher(s *store.Store) *Launcher {
	return &Launcher{
		store: s,
		ptys:  make(map[string]*os.File),
	}
}

// PTYMaster returns the PTY master file for the given session (for attach).
func (l *Launcher) PTYMaster(sessionID string) *os.File {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ptys[sessionID]
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

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open agent log: %w", err)
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()

	ptym, err := pty.Start(cmd)
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

// runAgent reads PTY output, writes to logFile, drives state transitions,
// and cleans up when the process exits.
func (l *Launcher) runAgent(sessionID string, cmd *exec.Cmd, ptym *os.File, logFile *os.File) {
	defer func() {
		ptym.Close()
		logFile.Close()
		l.mu.Lock()
		delete(l.ptys, sessionID)
		l.mu.Unlock()
	}()

	silenceTimer := time.NewTimer(SilenceTimeout)
	defer silenceTimer.Stop()

	gotOutput := make(chan struct{}, 1)

	// Goroutine: silence monitor.
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

	buf := make([]byte, 4096)
	for {
		n, err := ptym.Read(buf)
		if n > 0 {
			logFile.Write(buf[:n])
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

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 0 {
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
	l.mu.Lock()
	fds := make([]*os.File, 0, len(l.ptys))
	for _, f := range l.ptys {
		fds = append(fds, f)
	}
	l.mu.Unlock()

	for _, f := range fds {
		io.Copy(io.Discard, f)
	}

	return l.store.TerminateAllAgentSessions()
}
