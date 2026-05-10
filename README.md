# ticket

A local-first ticket tracker built for AI-human collaborative development. Agents pick up work, humans review and direct — `ticket` manages the state machine, git worktrees, and review threads that tie it all together.

## Goals

- **Agent-friendly CLI** — every state transition is a single command, easy to script from an agent loop
- **Human-controlled reviews** — humans drive the review→approve→merge path; agents can't bypass it
- **Iterative amendments** — threaded review comments automatically generate amendment tasks for the next round, so feedback is always traceable to specific work
- **Local-first** — SQLite on disk, no network dependencies, no accounts

## Workflow

```
draft → ready → in_progress → in_review → approved → merged
                                  ↕
                              (amendment rounds)
```

### 1. Human: create a ticket

```sh
ticket draft --title "Add login feature" --repo /path/to/repo
ticket task add T1 --title "Implement JWT auth" --verifiable-result "Tests pass"
ticket task add T1 --title "Add /login endpoint"
ticket ready T1 human:alice
```

### 2. Agent: work

The agent is dispatched by the TUI (`agent.auto_dispatch`) and pre-assigned to a specific ticket. It does the work and transitions to review:

```sh
# ... do the work (agent is pre-assigned via TUI dispatch) ...
ticket task complete <task-id>
ticket transition T1 in_review agent:claude
```

### 3. Human: review

Open the TUI (`ticket`) and navigate to the ticket in review. Use the threads screen (`t`) to leave comments, reply to threads, and mark issues as needing attention. Press `ctrl+s` to submit the review — this transitions the ticket back to `ready` and auto-generates amendment tasks for any unresolved threads.

### 4. Agent: amend

```sh
# ... address review feedback via the generated amendment tasks ...
ticket transition T1 in_review agent:claude
```

Repeat steps 3–4 until satisfied.

### 5. Human: approve and merge

```sh
# or use TUI keybindings: [a] approve, [m] merge
ticket approve T1 human:alice
ticket merge T1 human:alice    # fast-forward merges branch, cleans up worktree
```

## Installation

```sh
go install ./cmd/ticket
```

Requires Go 1.21+. The database is created automatically at `~/.local/share/ticket/tickets.db` on first run.

## TUI

Run `ticket` with no arguments to open the interactive interface.

| Key | Action |
|-----|--------|
| `↑/k` `↓/j` | Navigate ticket list |
| `r` | Mark draft ticket as ready |
| `R` | Revert ready ticket to draft |
| `t` | Open threads view |
| `[` / `]` | Scroll detail pane up / down |
| `g` | Dispatch agent |
| `enter` | Attach agent session |
| `shift+tab` | Cycle to next agent |
| `ctrl+]` | Detach agent |
| `a` | Approve in-review ticket |
| `m` | Merge approved ticket |
| `n` | Add note |
| `X` | Revert to draft (in-progress / in-review) |
| `C` | Add conflict-resolution task and requeue |
| `D` | Delete ticket |
| `q` | Quit |

**Threads view** (`t` from any ticket):

| Key | Action |
|-----|--------|
| `↑/k` `↓/j` | Navigate threads |
| `n` | New thread |
| `r` | Reply to thread |
| `x` | Resolve / reopen thread |
| `e` | Edit draft message |
| `D` | Delete draft message |
| `ctrl+s` | Submit review |
| `esc` | Back |

## CLI Reference

```sh
# Ticket lifecycle
ticket draft --title <title> --repo <path> [--description <text>] [--json]
ticket import [file]
ticket ls [--status <status>] [--json]
ticket get <id>
ticket transition <id> <status> <author>
ticket note add <ticket-id> <author> <text>
ticket ready <ticket-id> <author>
ticket redraft <ticket-id> <author>
ticket approve <id> <author>
ticket merge <id> <author>
ticket delete <id>
ticket purge --yes
ticket block <ticket-id> <blocker-id>
ticket unblock <ticket-id> <blocker-id>

# Tasks
ticket task add <ticket-id> --title <title> [--description <text>] [--verifiable-result <text>]
ticket task ls [--json] <ticket-id>
ticket task update <task-id> [--title <title>] [--description <text>] [--verifiable-result <text>]
ticket task move <task-id> <position>
ticket task complete <task-id>
ticket task uncomplete <task-id>

# Review
ticket review-submit <ticket-id> <author>
ticket thread reply <thread-id> <author> <text>
ticket thread transition <thread-id> <status> <author>

# Agent
ticket agent clear <ticket-id>

# Config
ticket config set <key> <value>
ticket config get <key>
ticket config ls
```

## Data model

- **Ticket** — unit of work; has status, feature branch, and worktree path
- **Task** — ordered step within a ticket; tagged with a `round` (1 = original, 2+ = amendments)
- **Thread** — review comment attached to a task; statuses: `open` → `needs_attention` → `resolved`
- **Draft state** — staged human edits (new threads, replies, resolve decisions) held until `review-submit`

## Configuration

```sh
ticket config set worktrees false         # disable automatic worktree creation
ticket config set agent.command 'claude --prompt {}'   # command used to dispatch agents
ticket config set agent.auto_dispatch true             # auto-dispatch agents when a ticket becomes ready
```

| Key | Default | Description |
|-----|---------|-------------|
| `worktrees` | `true` | Create a git worktree when a ticket becomes ready |
| `agent.command` | _(unset)_ | Command to run as an agent; `{}` is replaced with the dispatched skill file contents |
| `agent.auto_dispatch` | `false` | If `true`, automatically dispatch an agent whenever a ticket is promoted to `ready` |

`agent.command` must contain `{}` — the system substitutes the dispatched skill file contents in its place. Setting it without `{}` is a validation error.

Default config location: `~/.local/share/ticket/tickets.db` (same database, config table).

## Repository layout

```
cmd/ticket/        entry point — dispatches to cli or tui, launches TUI when run with no args
internal/
  cli/             CLI interface — one file per subcommand (draft, approve, merge, …)
  tui/             TUI interface — Bubbletea app, views, and reusable components
    views/         full-screen views (ticket list, ticket detail, threads)
    components/    reusable widgets (progress bar, status icon)
  workflow/        orchestration layer — promote, merge, redraft, review-submit
  store/           SQLite persistence — schema, migrations, per-entity CRUD
  model/           data types and state machine rules
  ids/             sequential ID generation (T1, T2, …)
```

`cli/` and `tui/` are parallel interfaces on top of the same `workflow/` and `store/` layer. Neither calls the other.
