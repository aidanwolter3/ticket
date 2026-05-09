package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeCodeWaitSignaler detects when Claude Code finishes a response and is
// waiting for user input, and when it starts a new tool use. It installs a
// project-level Stop hook and PreToolUse hook (.claude/settings.json) that
// touch signal files in the .agent directory.
type ClaudeCodeWaitSignaler struct {
	watcher    *signalFileWatcher
	runWatcher *signalFileWatcher
}

// NewClaudeCodeWaitSignaler creates a ClaudeCodeWaitSignaler for an agent
// running in worktreePath. If the hook file cannot be written it falls back to
// a NopWaitSignaler so that the silence timeout remains the detection mechanism.
func NewClaudeCodeWaitSignaler(worktreePath string) WaitSignaler {
	waitPath := filepath.Join(worktreePath, ".agent", "claude_waiting")
	runPath := filepath.Join(worktreePath, ".agent", "claude_running")

	if err := writeClaudeHooks(worktreePath, waitPath, runPath); err != nil {
		return NewNopWaitSignaler()
	}
	return &ClaudeCodeWaitSignaler{
		watcher:    newSignalFileWatcher(waitPath),
		runWatcher: newSignalFileWatcher(runPath),
	}
}

func (s *ClaudeCodeWaitSignaler) Chan() <-chan struct{}    { return s.watcher.Chan() }
func (s *ClaudeCodeWaitSignaler) RunChan() <-chan struct{} { return s.runWatcher.Chan() }
func (s *ClaudeCodeWaitSignaler) Close() {
	s.watcher.Close()
	s.runWatcher.Close()
}

// claudeSettings is the subset of .claude/settings.json that we write.
type claudeSettings struct {
	Hooks map[string][]claudeHookEntry `json:"hooks"`
}

type claudeHookEntry struct {
	Hooks []claudeHookCmd `json:"hooks"`
}

type claudeHookCmd struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// writeClaudeHooks writes .claude/settings.json with a Stop hook (touches
// waitPath) and a PreToolUse hook (touches runPath). It is a no-op if the file
// already exists to avoid clobbering existing project settings.
func writeClaudeHooks(worktreePath, waitPath, runPath string) error {
	claudeDir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}

	hookFile := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(hookFile); err == nil {
		return nil // already exists; don't overwrite
	}

	cfg := claudeSettings{
		Hooks: map[string][]claudeHookEntry{
			"Stop": {{
				Hooks: []claudeHookCmd{{
					Type:    "command",
					Command: "touch " + waitPath,
				}},
			}},
			"PreToolUse": {{
				Hooks: []claudeHookCmd{{
					Type:    "command",
					Command: "touch " + runPath,
				}},
			}},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(hookFile, data, 0o644)
}
