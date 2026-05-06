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
ticket draft "Add login feature" --repo /path/to/repo
ticket task add T1 --title "Implement JWT auth" --verifiable-result "Tests pass"
ticket task add T1 --title "Add /login endpoint"
ticket ready T1 human:alice
```

### 2. Agent: claim and work

```sh
ticket claim-work          # atomically claims next ready ticket, creates worktree
# ... do the work ...
ticket task complete <task-id>
ticket transition T1 in_review agent:claude
```

### 3. Human: review

Open the TUI (`ticket`) and navigate to the ticket in review. Use the threads screen (`t`) to leave comments, reply to threads, and mark issues as needing attention. Press `ctrl+s` to submit the review — this transitions the ticket back to `ready` and auto-generates amendment tasks for any unresolved threads.

### 4. Agent: amend

```sh
ticket claim-work          # claims the same ticket as amendment work
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
| `R` | Redraft (destroy worktree, return to draft) |
| `t` | Open threads view |
| `a` | Approve in-review ticket |
| `m` | Merge approved ticket |
| `n` | Add note |
| `D` | Delete ticket |
| `?` | Toggle help |
| `q` | Quit |

**Threads view** (`t` from any ticket):

| Key | Action |
|-----|--------|
| `↑/k` `↓/j` | Navigate threads |
| `n` | New thread |
| `r` | Reply to thread |
| `x` | Resolve / reopen thread |
| `ctrl+s` | Submit review |
| `esc` | Back |

## CLI Reference

```sh
# Ticket lifecycle
ticket draft <title> [--repo <path>] [--description <text>] [--branch <name>]
ticket ls [--status <status>]
ticket get <id>
ticket ready <id> <author>
ticket redraft <id> <author>
ticket approve <id> <author>
ticket merge <id> <author>
ticket delete <id>

# Tasks
ticket task add <ticket-id> --title <title> [--description <text>] [--verifiable-result <text>]
ticket task ls <ticket-id>
ticket task complete <task-id>
ticket task uncomplete <task-id>

# Review
ticket review-submit <ticket-id> <author>
ticket thread reply <thread-id> <author> <text>
ticket thread transition <thread-id> <status> <author>

# Work claiming (for agents)
ticket claim-work [--json]
ticket peek-work [--json]

# Config
ticket config set <key> <value>    # e.g. worktrees=false
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
ticket config set worktrees false   # disable automatic worktree creation
```

Default config location: `~/.local/share/ticket/tickets.db` (same database, config table).

## Repository layout

```
cmd/ticket/        entry point — dispatches to cli or tui, launches TUI when run with no args
internal/
  cli/             CLI interface — one file per subcommand (draft, approve, merge, …)
  tui/             TUI interface — Bubbletea app, views, and reusable components
    views/         full-screen views (ticket list, ticket detail, threads)
    components/    reusable widgets (progress bar, status icon)
  workflow/        orchestration layer — claim-work, merge, redraft, review-submit
  store/           SQLite persistence — schema, migrations, per-entity CRUD
  model/           data types and state machine rules
  ids/             sequential ID generation (T1, T2, …)
```

`cli/` and `tui/` are parallel interfaces on top of the same `workflow/` and `store/` layer. Neither calls the other.
