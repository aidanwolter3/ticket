package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeCodeWaitSignaler detects when Claude Code finishes a response and is
// waiting for user input. It installs a project-level Stop hook
// (.claude/settings.json) that touches a signal file in the .agent directory;
// a signalFileWatcher reports each touch as a waiting transition.
type ClaudeCodeWaitSignaler struct {
	watcher *signalFileWatcher
}

// NewClaudeCodeWaitSignaler creates a ClaudeCodeWaitSignaler for an agent
// running in worktreePath. If the hook file cannot be written it falls back to
// a NopWaitSignaler so that the silence timeout remains the detection mechanism.
func NewClaudeCodeWaitSignaler(worktreePath string) WaitSignaler {
	signalPath := filepath.Join(worktreePath, ".agent", "claude_waiting")

	if err := writeClaudeStopHook(worktreePath, signalPath); err != nil {
		return NewNopWaitSignaler()
	}
	return &ClaudeCodeWaitSignaler{watcher: newSignalFileWatcher(signalPath)}
}

func (s *ClaudeCodeWaitSignaler) Chan() <-chan struct{} { return s.watcher.Chan() }
func (s *ClaudeCodeWaitSignaler) Close()               { s.watcher.Close() }

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

// writeClaudeStopHook writes .claude/settings.json with a Stop hook that
// touches signalPath when Claude's turn ends. It is a no-op if the file
// already exists to avoid clobbering existing project settings.
func writeClaudeStopHook(worktreePath, signalPath string) error {
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
					Command: "touch " + signalPath,
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
