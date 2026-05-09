package agent

import (
	"os"
	"sync"
	"time"
)

// WaitSignaler reports transitions between waiting-for-user-input and running states.
type WaitSignaler interface {
	// Chan returns a channel that receives once each time the agent enters
	// the waiting-for-user-input state (e.g. Stop hook fired).
	Chan() <-chan struct{}
	// RunChan returns a channel that receives once each time the agent
	// re-enters the running state (e.g. PreToolUse hook fired).
	RunChan() <-chan struct{}
	// Close releases resources held by the signaler.
	Close()
}

// WaitSignalerFactory creates a WaitSignaler for a single agent launch.
// worktreePath is the agent's working directory.
type WaitSignalerFactory func(worktreePath string) WaitSignaler

// NopWaitSignaler never signals; the silence timeout is the sole mechanism
// for detecting the waiting state.
type NopWaitSignaler struct {
	ch    chan struct{}
	runCh chan struct{}
}

func NewNopWaitSignaler() *NopWaitSignaler {
	return &NopWaitSignaler{
		ch:    make(chan struct{}),
		runCh: make(chan struct{}),
	}
}

func (n *NopWaitSignaler) Chan() <-chan struct{}    { return n.ch }
func (n *NopWaitSignaler) RunChan() <-chan struct{} { return n.runCh }
func (n *NopWaitSignaler) Close()                  {}

// signalFileWatcher polls a file path; each time the file appears it sends on
// the channel and removes the file.
type signalFileWatcher struct {
	path string
	ch   chan struct{}
	done chan struct{}
	once sync.Once
}

const signalFilePollInterval = 100 * time.Millisecond

func newSignalFileWatcher(path string) *signalFileWatcher {
	w := &signalFileWatcher{
		path: path,
		ch:   make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	go w.poll()
	return w
}

func (w *signalFileWatcher) poll() {
	ticker := time.NewTicker(signalFilePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			if _, err := os.Stat(w.path); err == nil {
				os.Remove(w.path) //nolint:errcheck
				select {
				case w.ch <- struct{}{}:
				default:
				}
			}
		}
	}
}

func (w *signalFileWatcher) Chan() <-chan struct{} { return w.ch }

func (w *signalFileWatcher) Close() {
	w.once.Do(func() { close(w.done) })
}
