You have been pre-assigned to ticket **{{TICKET_ID}}**{{WORKTREE_CONTEXT}}. Do not run `ticket claim-work` or search for other work — begin immediately with step 1 below.

## Working directory

You are working inside a git worktree at `.worktrees/{{TICKET_ID}}/`. Always run commands from this worktree directory. Never `cd` to the parent repo or the main tree — the worktree is a complete, self-contained checkout of the feature branch.

## Status lifecycle

`draft → ready → in_progress → in_review → approved → merged`

Only humans approve and merge tickets. Agents claim work (`ready → in_progress`), finish it (`in_progress → in_review`), and flip amended threads back to open (`needs_attention → open`).

## Valid agent transitions

| From | To | Notes |
|---|---|---|
| `ready` | `in_progress` | via `claim-work` |
| `in_progress` | `in_review` | after all tasks committed |
| `in_review` | `in_progress` | to address review feedback |
| `needs_attention` thread | `open` thread | after posting amendment reply |

Human-only: `in_review → approved` (use `ticket approve`), `approved → merged` (use `ticket merge`).

## Workflow

### 1. Read full context

```bash
ticket get {{TICKET_ID}}
```

`ticket get` outputs **plain text**, not JSON — read it directly. Do not pipe it through `python`, `jq`, or any other parser.

Study every task (in `position` order), every thread, and every note before touching any code.

### 2. Execute the work

#### For new work

Implement each task in order. For each task:

1. Do the implementation.
2. Run the `verifiable_result` check. Fix any failures before continuing.
3. Commit **all changes for this task in a single commit**:
   - Commit on the feature branch (already set up in the worktree).
   - Commit message format: `<ticket-id> <task-id>: <task title>` — e.g. `T-012 TS-003: add argon2 hashing`
4. Mark the task complete:
   ```bash
   ticket task complete <task-id> agent:claude
   ```

Do not move to the next task until the current task's verifiable result passes, its commit is made, and it is marked complete.

#### For amendments

Review every thread on every task whose status is `ready`. For each such thread:

1. Read the full thread message history to understand what the reviewer asked for.
2. Make the requested change.
3. Run any relevant verifiable result checks.
4. Commit the fix: `<ticket-id> <task-id>: address review — <short summary>`
5. Reply to the thread:
   ```bash
   ticket thread reply <thread-id> agent:claude '<description of what was changed>'
   ```
6. Flip the thread back to open:
   ```bash
   ticket thread transition <thread-id> open agent:claude
   ```

### 3. Add a note (if warranted)

If you made any non-obvious decisions — a constraint, a tradeoff, a workaround — record them:

```bash
ticket note add <id> agent:claude '<note text>'
```

Skip this step if there is nothing non-obvious to say.

### 4. Hand off for review

```bash
ticket transition <id> in_review agent:claude
```

Then call:

```
ExitWorktree action:keep
```

### 5. Report to the user

Tell the user:
- Which ticket was completed and its title
- What was done (one sentence per task or amendment)
- The branch the commits landed on
- That it is now `in_review` and waiting for human approval

## Human-only commands (reference)

### ticket approve

Transitions `in_review → approved`. Requires no open threads.

```bash
ticket approve <id> human:<name>
```

### ticket merge

Fast-forward merges the feature branch into main, deletes the branch, removes the worktree, transitions to `merged`.

```bash
ticket merge <id> human:<name>
```

Preconditions: ticket is `approved`, all tasks complete, no open threads, `feature_branch` and `repo_path` are set. If the branch has diverged, the command errors — rebase manually then retry.

### ticket config

```bash
ticket config set <key> <value>
ticket config get <key>
```

Key `worktrees` (default `true`) controls whether promoting a ticket automatically creates a git worktree at `.worktrees/<ticket-id>/`.
