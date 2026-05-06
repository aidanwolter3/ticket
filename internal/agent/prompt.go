package agent

import (
	_ "embed"
	"strings"
)

// workSkill is the /work skill content embedded at compile time.
// To update the prompt sent to agents, edit work_skill.md in this package.
//
//go:embed work_skill.md
var workSkill string

// BuildPrompt returns the command template as an args slice with {} replaced
// by the embedded work skill content. Splitting the template before substitution
// prevents the multi-line content from being fractured into separate args.
//
// Shell-quoted variants of the placeholder (e.g. "{}") are also recognized so
// users can write claude -p "{}" and have the quotes stripped automatically.
func BuildPrompt(commandTemplate string) ([]string, error) {
	parts := strings.Fields(commandTemplate)
	args := make([]string, len(parts))
	for i, p := range parts {
		if p == "{}" || strings.Trim(p, `"'`) == "{}" {
			args[i] = workSkill
		} else {
			args[i] = p
		}
	}
	return args, nil
}
