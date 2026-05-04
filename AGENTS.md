# Agent Guide

This document explains how AI agents interact with the ticket tracker.
The TUI is the human surface; agents read and write through the CLI and database.

## Core model

```
Ticket  — unit of work or a plan grouping other tickets
Thread  — a comment conversation on a ticket (active → ready → resolved)
Note    — a free-form observation attached to a ticket
```

Status values: `draft` → `ready` → `in_progress` → `in_review` → `completed`

Only humans resolve threads and mark tickets completed. Agents claim work (`ready → in_progress`), finish it (`in_progress → in_review`), and flip amended threads back to active (`ready → active`).

## Finding available work

```bash
ticket ls --actionable --json
```

Returns a JSON array of ready tickets (not plans) whose blockers are all completed. This is the primary command for finding work.

As a fallback, you can filter manually:

```bash
ticket ls --status ready --json | jq '[.[] | select(.type == "ticket")]'
# then per-ticket get to check blockers
```

## Creating tickets (batch JSON import)

The primary way for agents to create tickets is `ticket import`. It accepts JSON on stdin or from a file.

### Planning workflow

Planning agents **must** file all tickets as `draft`. Only a human promotes tickets to `ready`.

1. Planning agent runs `ticket import` with all tickets set to `"status": "draft"`.
2. Human reviews the drafts in the TUI (Draft Review tab) or via `ticket ls --status draft --json`.
3. Human approves individual tickets (TUI `a` key) or bulk-promotes a whole plan:
   ```bash
   ticket promote <plan-id> human:<name>
   ```
4. Only `ready` tickets are picked up by implementation agents.

### JSON format

```json
{
  "tickets": [
    {
      "ref": "jwt",
      "title": "Add JWT validation",
      "type": "ticket",
      "status": "draft",
      "description": "Implement JWT validation middleware for all API routes.",
      "feature_branch": "feat/auth-argon2",
      "stack_id": "auth-1",
      "verifiable_result": "Run `npm test -- auth/jwt`. All tests pass.",
      "blocked_by": [],
      "notes": [
        { "author": "agent:claude", "text": "Use HS256 — we do not have an RSA key pair in this environment." }
      ]
    },
    {
      "ref": "bcrypt",
      "title": "Replace bcrypt with argon2",
      "type": "ticket",
      "status": "draft",
      "feature_branch": "feat/auth-argon2",
      "stack_id": "auth-1",
      "blocked_by": ["jwt"],
      "verifiable_result": "Run `npm test -- auth/`. All tests pass."
    },
    {
      "ref": "plan",
      "title": "Auth redesign",
      "type": "plan",
      "status": "draft",
      "blocked_by": ["jwt", "bcrypt"]
    }
  ]
}
```

**Field reference**

| Field | Required | Notes |
|---|---|---|
| `ref` | no | Local name for cross-referencing `blocked_by` within this document. Not stored. |
| `title` | **yes** | |
| `type` | no | `"ticket"` (default) or `"plan"` |
| `status` | no | Defaults to `"draft"`. Leave as `"draft"` — a human promotes to `"ready"` after review. |
| `description` | no | Markdown. Explain the work clearly — agents pick this up cold. |
| `feature_branch` | no | Git branch the work lives on. |
| `stack_id` | no | Tickets sharing a `stack_id` form a stack and are reviewed together. |
| `verifiable_result` | no | Markdown. How to confirm the work is done. |
| `blocked_by` | no | Array of `ref` values from this document, or existing ticket IDs like `"T-042"`. |
| `notes` | no | Array of `{ author, text }` — free-form observations. |
| `threads` | no | Array of `{ messages: [{ author, text }] }` — pre-seeded review threads. |

**Author format:** `"human:<name>"` or `"agent:<name>"` (e.g. `"agent:claude"`, `"human:aidan"`).

### Running the import

```bash
ticket import tickets.json
```

or pipe JSON directly:

```bash
echo '{"tickets":[{"title":"Fix login bug","type":"ticket","status":"ready"}]}' | ticket import
```

### Output

```json
{
  "created": {
    "jwt": "T-001",
    "bcrypt": "T-002",
    "plan": "T-003"
  }
}
```

The map keys are `ref` values (or `title` when no `ref` was given). Save these IDs if you need to reference the tickets in follow-up operations.

## Reading tickets

```bash
# All tickets as JSON
ticket ls --json

# Filter by status (JSON output)
ticket ls --status ready --json
ticket ls --status draft --json
ticket ls --status in_review --json

# Single ticket with full detail (threads, notes, blocked_by) — always JSON
ticket get T-042
```

Without `--json`, `ticket ls` prints a human-readable table. Always pass `--json` in agent scripts so output is machine-parseable.

## Transitioning status

```bash
ticket transition <id> <new-status> <author>
```

Examples:

```bash
# Claim a ticket
ticket transition T-001 in_progress agent:claude

# Mark work done and ready for human review
ticket transition T-001 in_review agent:claude

# Not allowed — only humans do this:
# ticket transition T-001 completed agent:claude  ← will error
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
ticket ls --actionable --json

# 2. Claim it
ticket transition T-043 in_progress agent:claude

# 3. Read full context
ticket get T-043

# 4. Do the work — implement changes, then commit

# 5. Add a note summarising any non-obvious decisions
ticket note add T-043 agent:claude 'Explain any non-obvious decisions here.'

# 6. Hand off for review
ticket transition T-043 in_review agent:claude
```

#### Committing work

Agents **must** commit all changes before transitioning a ticket to `in_review`.

- **Branch**: if the ticket has a `feature_branch`, commit on that branch. If no branch is set, commit on the current branch.
- **Commit message**: reference the ticket ID in the subject line, e.g.:
  ```
  T-043: add JWT validation middleware
  ```
- **Stacks**: if a `stack_id` is set, all tickets in the stack share the same `feature_branch`. Commits from each ticket land on that branch and are reviewed together as a unit. Do not merge or rebase the branch between tickets in the same stack.

### Handling amendment requests (in_review → ready tickets with ready threads)

```bash
# 1. Find tickets that are ready with a commit (amendment work)
ticket get T-043   # check commit_hash is set and threads exist

# 2. Claim the stack / ticket
ticket transition T-043 in_progress agent:claude

# 3. Read the ready threads and make the changes

# 4. For each amended thread, post a reply and flip it back to active
ticket thread reply <thread-id> agent:claude 'Fixed in commit abc123.'
ticket thread transition <thread-id> active agent:claude
# then mark ticket in_review again
ticket transition T-043 in_review agent:claude
```

## Tips for writing good tickets

- **description**: explain the problem, not the solution. Give enough context for a cold agent to understand the intent.
- **verifiable_result**: make it a concrete, runnable check. "All tests pass" is weak; "`npm test -- auth/jwt` exits 0 and output includes `5 passing`" is strong.
- **stack_id**: use a consistent short string for all tickets on the same branch (e.g. `auth-1`). Stacks are reviewed as a unit.
- **blocked_by on plans**: add all child ticket refs so the plan shows progress correctly in the TUI.
- **notes**: add agent notes as you work — future agents on the same stack will read them.
