// periodic_signal_agent is a test helper used by launcher_test.go.
// It writes the claude_waiting signal file, then emits periodic output every
// 600ms to verify that the debounce gate prevents those outputs from flipping
// state back to AgentRunning.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	fmt.Println("periodic_signal_agent: starting")
	time.Sleep(50 * time.Millisecond)

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "periodic_signal_agent: getwd:", err)
		os.Exit(1)
	}
	signalPath := filepath.Join(cwd, ".agent", "claude_waiting")
	if err := os.WriteFile(signalPath, nil, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "periodic_signal_agent: write signal:", err)
		os.Exit(1)
	}
	fmt.Println("periodic_signal_agent: signalled waiting")

	// Emit output every 600ms for ~3 seconds to simulate periodic PTY output
	// after the Stop hook fires (e.g. Claude re-rendering its input prompt).
	for i := 0; i < 5; i++ {
		time.Sleep(600 * time.Millisecond)
		fmt.Printf("periodic_signal_agent: tick %d\n", i+1)
	}
	fmt.Println("periodic_signal_agent: done")
}
