# Agent Guide

This document explains how AI agents interact with the ticket tracker.
The TUI is the human surface; agents read and write through the CLI and database.

## Core model

```
Ticket  — unit of work assigned to one agent; groups an ordered sequence of tasks
Task    — a single leaf work item inside a ticket; maps to one git commit
Thread  — a comment conversation on a task (active → ready → resolved)
Note    — a free-form observation attached to a ticket
```

Status values: `draft` → `ready` → `in_progress` → `in_review` → `completed`

Only humans resolve threads and mark tickets completed. Agents claim work (`ready → in_progress`), finish it (`in_progress → in_review`), and flip amended threads back to active (`ready → active`).

## Finding available work

```bash
go run ./cmd/ticket find-work
```

Returns a JSON array of work items. Two types are returned:

- **`new_work`** — a `ready` ticket whose blockers are all completed (nothing started yet)
- **`amendment`** — an `in_review` ticket with one or more `ready` threads waiting for changes

Each item includes the full ticket context (tasks, threads, notes) and a plain-English `instructions` field explaining exactly what to do.

Example output:

```json
[
  {
    "type": "new_work",
    "ticket": {
      "id": "T-001",
      "title": "Auth redesign",
      "status": "ready",
      "feature_branch": "feat/auth-argon2",
      "tasks": [
        {
          "id": "TS-001",
          "title": "Add JWT validation",
          "position": 1,
          "verifiable_result": "Run `npm test -- auth/jwt`. All tests pass."
        }
      ]
    },
    "instructions": "Implement the tasks in order..."
  }
]
```

## Creating tickets (batch JSON import)

The primary way for agents to create tickets is `go run ./cmd/ticket import`. It accepts JSON on stdin or from a file.

### Planning workflow

Planning agents **must** file all tickets as `draft`. Only a human promotes tickets to `ready`.

1. Planning agent runs `go run ./cmd/ticket import` with all tickets set to `"status": "draft"`.
2. Human reviews the drafts in the TUI (Draft Review tab) or via `go run ./cmd/ticket ls --status draft --json`.
3. Human approves individual tickets (TUI `a` key) or promotes a ticket:
   ```bash
   go run ./cmd/ticket promote <ticket-id> human:<name>
   ```
4. Only `ready` tickets are picked up by implementation agents.

### JSON format

```json
{
  "tickets": [
    {
      "ref": "auth",
      "title": "Auth redesign",
      "status": "draft",
      "description": "Implement the full auth redesign: JWT validation, argon2 hashing, and session migration.",
      "feature_branch": "feat/auth-argon2",
      "blocked_by": [],
      "tasks": [
        {
          "title": "Add JWT validation",
          "description": "Implement JWT validation middleware for all API routes.",
          "verifiable_result": "Run `npm test -- auth/jwt`. All tests pass."
        },
        {
          "title": "Replace bcrypt with argon2",
          "verifiable_result": "Run `npm test -- auth/`. All tests pass. Login works with both old and new hashes."
        },
        {
          "title": "Migrate user sessions",
          "verifiable_result": "Run `npm test -- auth/sessions`. All sessions migrate without forced logouts."
        }
      ],
      "notes": [
        { "author": "agent:claude", "text": "Use HS256 — we do not have an RSA key pair in this environment." }
      ]
    }
  ]
}
```

**Ticket field reference**

| Field | Required | Notes |
|---|---|---|
| `ref` | no | Local name for cross-referencing `blocked_by` within this document. Not stored. |
| `title` | **yes** | |
| `status` | no | Defaults to `"draft"`. Leave as `"draft"` — a human promotes to `"ready"` after review. |
| `description` | no | Markdown. Explain the work clearly — agents pick this up cold. |
| `feature_branch` | no | Git branch the work lives on. |
| `blocked_by` | no | Array of `ref` values from this document, or existing ticket IDs like `"T-042"`. |
| `tasks` | no | Ordered array of task objects (see below). |
| `notes` | no | Array of `{ author, text }` — free-form observations. |
| `threads` | no | Array of `{ messages: [{ author, text }] }` — pre-seeded review threads on the first task. |

**Task field reference**

| Field | Required | Notes |
|---|---|---|
| `title` | **yes** | |
| `description` | no | Markdown detail for this specific task. |
| `verifiable_result` | no | Concrete, runnable check. "`npm test -- auth/jwt` exits 0" is better than "tests pass". |
| `threads` | no | Array of `{ messages: [{ author, text }] }` — pre-seeded threads on this task. |

**Author format:** `"human:<name>"` or `"agent:<name>"` (e.g. `"agent:claude"`, `"human:aidan"`).

### Running the import

```bash
go run ./cmd/ticket import tickets.json
```

or pipe JSON directly:

```bash
echo '{"tickets":[{"title":"Fix login bug","status":"ready","tasks":[{"title":"Patch the handler"}]}]}' | go run ./cmd/ticket import
```

### Output

```json
{
  "created": {
    "auth": "T-001"
  }
}
```

The map keys are `ref` values (or `title` when no `ref` was given). Save these IDs if you need to reference the tickets in follow-up operations.

## Reading tickets

```bash
# All tickets as JSON
go run ./cmd/ticket ls --json

# Filter by status (JSON output)
go run ./cmd/ticket ls --status ready --json
go run ./cmd/ticket ls --status draft --json
go run ./cmd/ticket ls --status in_review --json

# Single ticket with full detail (tasks, threads, notes, blocked_by) — always JSON
go run ./cmd/ticket get T-042
```

Without `--json`, `ls` prints a human-readable table. Always pass `--json` in agent scripts so output is machine-parseable.

## Transitioning status

```bash
go run ./cmd/ticket transition <id> <new-status> <author>
```

Examples:

```bash
# Claim a ticket
go run ./cmd/ticket transition T-001 in_progress agent:claude

# Mark work done and ready for human review
go run ./cmd/ticket transition T-001 in_review agent:claude

# Not allowed — only humans do this:
# go run ./cmd/ticket transition T-001 completed agent:claude  ← will error
```

Valid transitions for agents:

| From | To | Allowed? |
|---|---|---|
| `ready` | `in_progress` | yes |
| `in_progress` | `in_review` | yes |
| `in_review` | `ready` | **human only** |
| `in_review` | `completed` | **human only** |
| `active` thread | `ready` thread | **human only** |
| `ready` thread | `active` thread | yes (after posting amendment reply) |
| any thread | `resolved` | **human only** |

## Typical agent workflow

### Picking up and executing a ticket

```bash
# 1. Find available work
go run ./cmd/ticket find-work

# 2. Claim it
go run ./cmd/ticket transition T-001 in_progress agent:claude

# 3. Read full context (tasks, threads, notes)
go run ./cmd/ticket get T-001

# 4. Implement each task in order — one commit per task

# 5. Add a note summarising any non-obvious decisions
go run ./cmd/ticket note add T-001 agent:claude 'Explain any non-obvious decisions here.'

# 6. Hand off for review
go run ./cmd/ticket transition T-001 in_review agent:claude
```

#### Committing work

Agents **must** commit all changes before transitioning a ticket to `in_review`.

- **One commit per task**: each task in the ticket maps to exactly one git commit. Complete tasks in the order given by `position`.
- **Branch**: if the ticket has a `feature_branch`, commit on that branch. If no branch is set, commit on the current branch.
- **Commit message**: reference the ticket ID and task title in the subject line, e.g.:
  ```
  T-001 TS-002: replace bcrypt with argon2
  ```
- **Verifiable result**: after each task, run the `verifiable_result` check before moving to the next task.

### Handling amendment requests

When `find-work` returns an `amendment` item, one or more threads on the ticket's tasks have been flipped to `ready` by a human reviewer.

```bash
# 1. find-work will surface the amendment automatically
go run ./cmd/ticket find-work

# 2. Claim the ticket
go run ./cmd/ticket transition T-001 in_progress agent:claude

# 3. Read full context — review ready threads on each task
go run ./cmd/ticket get T-001

# 4. Make the requested changes

# 5. For each amended thread, post a reply and flip it back to active
go run ./cmd/ticket thread reply <thread-id> agent:claude 'Fixed in latest commit.'
go run ./cmd/ticket thread transition <thread-id> active agent:claude

# 6. Mark the ticket in_review again
go run ./cmd/ticket transition T-001 in_review agent:claude
```

## Tips for writing good tickets

- **description**: explain the problem, not the solution. Give enough context for a cold agent to understand the intent.
- **tasks**: break work into ordered steps that each produce one meaningful commit. Smaller tasks make review easier.
- **verifiable_result**: make it a concrete, runnable check. "All tests pass" is weak; "`npm test -- auth/jwt` exits 0 and output includes `5 passing`" is strong.
- **blocked_by**: list any ticket IDs that must complete before this ticket can start. `find-work` uses this to filter out blocked tickets.
- **notes**: add agent notes as you work — future agents on the same ticket will read them.
