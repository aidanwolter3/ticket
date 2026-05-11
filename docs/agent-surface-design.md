# Design: Agent Command Surface (`--agent` flag)

## Decision: `ticket --agent <subcommand>` flag-based approach

Agent commands are gated behind a top-level `--agent` token, which is the first argument
after `ticket`. This mirrors `git --no-pager` and `brew --verbose` — a single prefix shifts
behavioral mode without introducing a new subcommand namespace collision.

### Examples

```bash
ticket --agent in-progress T-134
ticket --agent in-review T-134
ticket --agent task complete --most-recent-commit TS-339
ticket --agent task uncomplete TS-339
ticket --agent task set-commit TS-339 <hash>
ticket --agent --help
```

### Why not `ticket agent <subcommand>`?

`ticket agent` already exists as the namespace for `ticket agent clear`. Reusing it for
routing *all* agent commands would require nesting it further (`ticket agent in-review`,
`ticket agent task complete`) which is awkward because `task` is a top-level noun, not an
`agent` sub-noun. The flag form keeps the existing subcommand grammar intact while simply
scoping which commands are visible.

### Help text structure

`ticket --help` (default, human mode):
- Shows TUI, draft, import, ls, get, update, ready, redraft, note, thread, task (human
  subcommands only), block, unblock, config, delete, purge, agent clear.
- Does NOT show: in-progress, in-review, task complete, task uncomplete, task set-commit.

`ticket --agent --help`:
- Shows only the agent-facing command surface.
- Lists: in-progress, in-review, task complete, task uncomplete, task set-commit.
- Omits human-only commands (draft, import, ready, redraft, config, delete, purge, etc.).

### Agent-only commands (hidden from human help)

| Command | Transition / Effect |
|---|---|
| `in-progress` | ready → in_progress |
| `in-review` | in_progress → in_review |
| `task complete` | marks task done, records commit hash |
| `task uncomplete` | reverses completion |
| `task set-commit` | updates commit hash after autosquash rebase |
| `note add` | adds a note to a ticket |

### Shared commands (remain in both surfaces)

`ls`, `get`, `thread reply`, `thread transition`, `task ls`, `task get`
are kept accessible without `--agent` because humans use them interactively and agents
use them for reads or shared writes (e.g. posting amendment replies).

`note add` was previously shared but is now gated behind `--agent` to enforce least-privilege
boundaries; agents must use `ticket --agent note add`.

### Migration plan

**Hard cutover** (no deprecation period):

- The existing bare forms (`ticket in-review`, `ticket task complete`, etc.) are removed
  from the main switch and moved exclusively behind `--agent`.
- The dispatched agent skill file and any harness invocations are updated in the same PR
  (TS-342) before the feature lands, so there is no window where old invocations break.
- No deprecation warning or compatibility shim is needed — all call sites are in
  version-controlled skill files that are updated atomically with the implementation.

## Workflow package architecture

After the refactor, the orchestration layer is split into two subpackages:

```
internal/workflow/
  human/   — all operations available to CLI and TUI: Draft, Ready, Redraft, Merge,
              task/thread management, config, and read-only store proxies.
              Also exposes agent-transition methods (StartWork, SubmitForReview,
              CompleteTask, AddNote) so the CLI layer has a single workflow entry point.
  agent/   — standalone agent workflow functions that operate directly on the store;
              used internally by the human workflow or independently in tests.
```

### Least-privilege enforcement

- `internal/cli/` and `internal/tui/` import only `internal/workflow/human` — never
  `internal/store` directly. A `depguard` linter rule enforces this boundary.
- The `human.Workflow` struct owns the `*store.Store` and exposes only typed methods;
  no raw store handle leaks out to CLI or TUI callers.
- Agent-only commands (in-progress, in-review, task complete, etc.) are routed through
  `runAgent()` in `main.go` and dispatched to `cli.RunInProgress`, `cli.RunInReview`,
  `cli.RunAgentTask`, and `cli.RunNote` — all of which accept `*human.Workflow`.

### Approve and merge (human-only, TUI-only)

`ticket approve` and `ticket merge` are not exposed as CLI subcommands. Humans approve
and merge tickets via the TUI (`[a]` and `[m]` keybindings). The workflow logic lives in
`internal/workflow/human.Merge()`; there is no CLI shim.
