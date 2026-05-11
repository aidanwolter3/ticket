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
  subcommands only), block, unblock, config, delete, purge, review-submit, agent clear.
- Does NOT show: in-progress, in-review, task complete, task uncomplete, task set-commit.

`ticket --agent --help`:
- Shows only the agent-facing command surface.
- Lists: in-progress, in-review, task complete, task uncomplete, task set-commit.
- Omits human-only commands (draft, import, ready, redraft, config, delete, purge, etc.).

### Agent-only commands (hidden from human help)

| Command | Transition |
|---|---|
| `in-progress` | ready → in_progress |
| `in-review` | in_progress → in_review |
| `task complete` | marks task done, records commit hash |
| `task uncomplete` | reverses completion |
| `task set-commit` | updates commit hash after autosquash rebase |

### Shared commands (remain in both surfaces)

`ls`, `get`, `note add`, `thread reply`, `thread transition`, `task ls`, `task get`
are kept accessible without `--agent` because humans use them interactively and agents
use them for reads or shared writes (e.g. posting amendment replies).

### Migration plan

**Hard cutover** (no deprecation period):

- The existing bare forms (`ticket in-review`, `ticket task complete`, etc.) are removed
  from the main switch and moved exclusively behind `--agent`.
- The dispatched agent skill file and any harness invocations are updated in the same PR
  (TS-342) before the feature lands, so there is no window where old invocations break.
- No deprecation warning or compatibility shim is needed — all call sites are in
  version-controlled skill files that are updated atomically with the implementation.
