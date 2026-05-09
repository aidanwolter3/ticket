// signal_agent is a test helper used by launcher_test.go.
// It simulates a Claude Code-style wait signal: it writes a signal file to
// .agent/claude_waiting in its working directory, then exits.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	fmt.Println("signal_agent: starting")
	time.Sleep(50 * time.Millisecond)

	// Simulate the Claude Code Stop hook writing the signal file.
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "signal_agent: getwd:", err)
		os.Exit(1)
	}
	signalPath := filepath.Join(cwd, ".agent", "claude_waiting")
	if err := os.WriteFile(signalPath, nil, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "signal_agent: write signal:", err)
		os.Exit(1)
	}

	fmt.Println("signal_agent: signalled waiting")
	// Stay alive long enough for the watcher (100 ms poll) to detect the file.
	time.Sleep(300 * time.Millisecond)
	fmt.Println("signal_agent: done")
}
