---
name: ticket
description: File a new ticket in the tracker. Use when the user wants to create a ticket, plan work, or draft tasks. Accepts a description of the work as args.
---

Create a new ticket for the work described in the args (or ask the user what the ticket should cover if no args were given).

## Core model

```
Ticket  — unit of work assigned to one agent; groups an ordered sequence of tasks
Task    — a single leaf work item inside a ticket; maps to one git commit
Thread  — a comment conversation on a task (active → ready → resolved)
Note    — a free-form observation attached to a ticket
```

Status lifecycle: `draft → ready → in_progress → in_review → approved → merged`

Only humans promote, approve, and merge tickets. Agents claim work and transition to `in_review`.

## Listing and reading tickets

```bash
ticket ls                         # human-readable table of all tickets
ticket ls --status draft          # filter by status
ticket ls --json                  # machine-readable JSON (use in scripts)
ticket get <id>                   # full detail: tasks, threads, notes, blocked_by
ticket get --json <id>            # machine-readable JSON with full task objects (id, title, description, position, verifiable_result, completed_at)
```

Note: the subcommand is `ls`, not `list`.

`ticket get` outputs **plain text**, not JSON — read it directly. Do not pipe it through `python`, `jq`, or any other parser. Use `ticket get --json <id>` to get machine-readable output including the full tasks array with all fields. This is the preferred way to enumerate task IDs and details without a separate `ticket task ls` call.

## Creating tickets

Tickets are always created as `draft`. Only a human promotes them to `ready`.

### draft (primary path for agents)

Use `ticket draft` to create a ticket with flag arguments — no JSON quoting needed:

```bash
ticket draft --title "Your title here" --repo "$(git rev-parse --show-toplevel)"
# outputs: T-047
```

With a description:

```bash
ticket draft --title "Your title here" \
  --repo "$(git rev-parse --show-toplevel)" \
  --description "Explain the problem here."
```

Output is the assigned ticket ID (e.g. `T-047`). Pass `--json` to get the full ticket JSON instead.

Add tasks afterward with `ticket task add`, using the literal ID from the output:

```bash
ticket task add T-047 --title "First task title" \
  --description "What to do" \
  --verifiable-result "go test ./... exits 0"
```

### import (batch / migration path)

Use `ticket import` with a heredoc for bulk creation or migration:

```bash
ticket import <<'EOF'
{
  "tickets": [
    {
      "title": "...",
      "status": "draft",
      "description": "...",
      "feature_branch": "feat/...",
      "repo_path": "/absolute/path/to/repo",
      "blocked_by": [],
      "tasks": [
        {
          "title": "...",
          "description": "...",
          "verifiable_result": "..."
        }
      ]
    }
  ]
}
EOF
```

## Updating and reordering tasks

Use `ticket task update` to change a task's fields in-place without altering its ID or position:

```bash
ticket task update <task-id> --title "New title"
ticket task update <task-id> --description "New description" --verifiable-result "go test ./... exits 0"
```

At least one of `--title`, `--description`, or `--verifiable-result` must be provided. Omitted fields are unchanged.

Use `ticket task move` to reorder a task within its ticket:

```bash
ticket task move <task-id> <position>   # position is 1-based
```

All other tasks shift to accommodate. Moving to position 1 pushes the current first task to position 2, and so on. Positions outside `[1, len(tasks)]` return an error.

**Note:** With both commands, the `--db` flag (if needed) must come after the task ID:
```bash
ticket task update <task-id> --db /path/to/db --title "New title"
ticket task move <task-id> --db /path/to/db 2
```

These commands replace the delete-and-recreate workaround, which changes the task ID and requires re-adding all subsequent tasks to restore order.

### Ticket field reference

| Field | Required | Notes |
|---|---|---|
| `title` | **yes** | |
| `description` | no | Markdown. Explain the problem, not the solution. Enough context for a cold agent. |
| `feature_branch` | no | Git branch for the work. Auto-generated as `feat/<lowercase-id>` if omitted. |
| `repo_path` | **yes** | Absolute path to the git repo. Resolve with `git rev-parse --show-toplevel`. |
| `blocked_by` | no | Array of existing ticket IDs (e.g. `"T-042"`). Add after creation via `ticket block`. |

### Task field reference

| Field | Required | Notes |
|---|---|---|
| `title` | **yes** | |
| `description` | no | Markdown detail for this specific task. |
| `verifiable_result` | no | Concrete, runnable check. "`go test ./...` exits 0" beats "tests pass". |

**Author format:** `"human:<name>"` or `"agent:<name>"` (e.g. `"agent:claude"`, `"human:aidan"`).

## Promoting and blocking

```bash
ticket ready <id> human:<name>          # draft → ready (also creates worktree when worktrees=true)
ticket block <ticket-id> <blocker-id>   # record that ticket-id is blocked by blocker-id
ticket unblock <ticket-id> <blocker-id> # remove the dependency
```

## Tips for writing good tickets

- **description**: explain the problem, not the solution. Give enough context for a cold agent.
- **tasks**: ordered steps, each producing one meaningful commit. Smaller tasks make review easier.
- **verifiable_result**: make it a concrete, runnable check. "`go test ./...` exits 0" is better than "tests pass".
- **blocked_by**: list ticket IDs that must complete before this ticket can start. `claim-work` treats `approved` or `merged` as satisfying a blocker.
- **notes**: add agent notes as you work — future agents on the same ticket will read them.

## Your job

Produce a well-formed ticket using `ticket draft` and `ticket task add`.

### Steps

1. **Understand the work.** Read the args. If they are vague or missing, ask one focused question to clarify scope — then proceed. Do not ask multiple questions at once.

2. **Check for duplicates.** Run `ticket ls --json` to fetch all tickets. Filter out any with `"status": "backlogged"`. Compare the proposed work against every remaining ticket's title and description. If any existing ticket covers substantially the same work:
   - Name the likely duplicate(s) and briefly explain the overlap.
   - Ask the user to choose: **(a)** proceed with a new ticket anyway, **(b)** update the existing ticket instead, or **(c)** cancel.
   - Only continue to the next step if the user selects (a). If they select (b), help them update the existing ticket and stop. If they select (c), stop.
   - If no significant overlap is found, proceed silently without mentioning the check.

3. **Identify likely blockers.** Using the same ticket list from step 2 (re-run `ticket ls --json` if needed), examine non-backlogged tickets in `draft`, `ready`, or `in_progress` status. Identify any that appear to be prerequisites for the new work based on title/description overlap or logical ordering. For each candidate:
   - Name the ticket and explain why it looks like a prerequisite.
   - Ask the user to confirm whether it should be recorded as a blocker.
   - Keep track of confirmed blocker IDs — you will call `ticket block` after creation.
   - If no likely prerequisites are found, proceed silently without mentioning the check.

4. **Design the ticket.** Apply these rules:
   - Tickets are always created as `draft` — the human promotes to `ready` after review.
   - `description`: explain the problem and context, not the solution. Enough for a cold agent to understand intent.
   - `tasks`: ordered steps, each producing one meaningful commit. Smaller tasks make review easier.
   - `verifiable_result`: a concrete runnable check per task (e.g. `go test ./... exits 0`), not "tests pass".
   - `feature_branch` (`--branch`): use `feat/<slug>` naming when the work warrants its own branch.
   - `repo_path` (`--repo`): always set this — run `git rev-parse --show-toplevel` to get the absolute path.
   - Only include a description if there is genuine context to convey.

5. **Create the ticket.** Use `ticket draft` as shown above. Read the ticket ID from stdout and use it literally in subsequent commands.

6. **Add tasks.** Every ticket must have at least one task — a ticket with no tasks cannot be worked. For each task, call `ticket task add <id> --title ... [--description ...] [--verifiable-result ...]`. If the work is small or not yet fully scoped, add a single task that captures the core change.

7. **Add blocking relationships.** For each blocker ID confirmed in step 3, run:
   ```bash
   ticket block <new-ticket-id> <blocker-id>
   ```

8. **Confirm.** Tell the user the ticket ID and title. Remind them it is in `draft` — they can promote it in the TUI or run `ticket ready <id> human:<name>`. If any blockers were added, mention them.
