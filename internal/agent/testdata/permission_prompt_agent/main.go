// permission_prompt_agent is a test helper used by launcher_test.go.
// It simulates what happens when Claude Code shows a permission prompt: the
// Notification/permission_prompt hook touches .agent/claude_waiting, then the
// agent stays alive briefly before exiting.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	fmt.Println("permission_prompt_agent: starting")
	time.Sleep(50 * time.Millisecond)

	// Simulate the fixed Notification/permission_prompt hook touching the signal file.
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "permission_prompt_agent: getwd:", err)
		os.Exit(1)
	}
	waitPath := filepath.Join(cwd, ".agent", "claude_waiting")
	if err := os.WriteFile(waitPath, nil, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "permission_prompt_agent: write signal:", err)
		os.Exit(1)
	}

	fmt.Println("permission_prompt_agent: permission prompt shown")
	// Stay alive long enough for the watcher (100 ms poll) to detect the file.
	time.Sleep(500 * time.Millisecond)
	fmt.Println("permission_prompt_agent: done")
}
