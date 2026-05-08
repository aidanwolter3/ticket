package agent

import (
	_ "embed"
	"fmt"
	"strings"
)

// workSkill is the /work skill content embedded at compile time.
// To update the prompt sent to agents, edit work_skill.md in this package.
//
//go:embed work_skill.md
var workSkill string

// dispatchedSkill is the pre-assigned agent skill embedded at compile time.
// It contains {{TICKET_ID}} and {{WORKTREE_CONTEXT}} placeholders that
// BuildTicketPrompt replaces at runtime.
//
//go:embed dispatched_skill.md
var dispatchedSkill string

// BuildTicketPrompt builds a ticket-specific prompt from the dispatched skill
// template, substituting the ticket ID and worktree context. The result is a
// self-contained prompt that starts at "Read full context" — it omits the
// claim-work and enter-worktree steps since the agent is already positioned.
func BuildTicketPrompt(ticketID, worktreePath string) string {
	worktreeCtx := ""
	if worktreePath != "" {
		worktreeCtx = " and are already inside the correct worktree at " + worktreePath
	}
	result := strings.ReplaceAll(dispatchedSkill, "{{TICKET_ID}}", ticketID)
	result = strings.ReplaceAll(result, "{{WORKTREE_CONTEXT}}", worktreeCtx)
	return result
}

// BuildPrompt builds the command args to run an agent. It replaces the {}
// placeholder (or "{}" / '{}') in the template with a reference to the
// TICKET_AGENT_PROMPT environment variable, then wraps the whole command in
// "bash -c" so the shell handles quoting. The actual prompt text is passed
// through the environment to avoid any argument-splitting or quoting issues.
//
// The caller must add TICKET_AGENT_PROMPT=<workSkill> to the subprocess env;
// Launcher.Launch does this automatically.
//
// Returns an error if the placeholder is not found or the embedded skill is empty.
func BuildPrompt(commandTemplate string) ([]string, error) {
	if workSkill == "" {
		return nil, fmt.Errorf("embedded work skill is empty — rebuild the binary")
	}

	// Detect which form of the placeholder is present.
	var shellCmd string
	switch {
	case strings.Contains(commandTemplate, `"{}"`):
		shellCmd = strings.ReplaceAll(commandTemplate, `"{}"`, `"$TICKET_AGENT_PROMPT"`)
	case strings.Contains(commandTemplate, `'{}'`):
		shellCmd = strings.ReplaceAll(commandTemplate, `'{}'`, `"$TICKET_AGENT_PROMPT"`)
	case strings.Contains(commandTemplate, `{}`):
		shellCmd = strings.ReplaceAll(commandTemplate, `{}`, `"$TICKET_AGENT_PROMPT"`)
	default:
		return nil, fmt.Errorf("command template %q contains no '{}' placeholder", commandTemplate)
	}

	return []string{"/bin/bash", "-c", shellCmd}, nil
}
