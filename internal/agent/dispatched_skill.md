You have been pre-assigned to ticket **{{TICKET_ID}}**{{WORKTREE_CONTEXT}}. Begin immediately with step 1 below.

## Working directory

You are working inside a git worktree at `.worktrees/{{TICKET_ID}}/`. Always run commands from this worktree directory. Never `cd` to the parent repo or the main tree — the worktree is a complete, self-contained checkout of the feature branch.

The harness has already entered the worktree before you started. Do not call `EnterWorktree` or `ExitWorktree` — the harness manages the entire worktree lifecycle. Simply run commands from your current working directory.

Never use `git -C <path>` when your working directory is already that path — run `git` commands directly. The `-C` flag triggers an unnecessary permission prompt when the working directory is already correct.

Never run `git push` or any command that writes to a remote. Pushing is reserved for the human.

## Status lifecycle

`draft → ready → in_progress → in_review → approved → merged`

Only humans approve and merge tickets. Agents are dispatched to tickets and transition them (`in_progress → in_review`), and flip amended threads back to open (`needs_attention → open`).

## Valid agent transitions

| From | To | Notes |
|---|---|---|
| `ready` | `in_progress` | via TUI dispatch |
| `in_progress` | `in_review` | after all tasks committed |
| `in_review` | `in_progress` | via TUI re-dispatch |
| `needs_attention` thread | `open` thread | after posting amendment reply |

Human-only: `in_review → approved` (use `ticket approve`), `approved → merged` (use `ticket merge`).

## Workflow

### 1. Read full context

```bash
ticket get {{TICKET_ID}}
```

`ticket get` outputs **plain text**, not JSON — read it directly. Do not pipe it through `python`, `jq`, or any other parser.

The plain-text output truncates long task titles and **omits thread messages entirely**. To read thread content (e.g., when addressing review feedback), use the `--json` flag — which must come **before** the ID:

```bash
ticket get --json {{TICKET_ID}}
```

This outputs full JSON including all thread messages, complete task titles, and notes.

Study every task (in `position` order), every thread, and every note before touching any code.

### 2. Execute the work

#### For new work

Implement each task in order. For each task:

1. Do the implementation.
2. Run the `verifiable_result` check. Fix any failures before continuing.
3. Commit **all changes for this task in a single commit**:
   - Commit on the feature branch (already set up in the worktree).
   - Commit message format: `<ticket-id> <task-id>: <task title>` — e.g. `T-012 TS-003: add argon2 hashing`
   - Capture the commit hash immediately after committing:
     ```bash
     COMMIT_HASH=$(git rev-parse HEAD)
     ```
4. Mark the task complete:
   - If the task has `no_commit: true` (verification-only, no code change), complete without a hash:
     ```bash
     ticket task complete <task-id>
     ```
   - Otherwise (the default), record the commit hash:
     ```bash
     ticket task complete <task-id> --commit $COMMIT_HASH
     ```
     You may also use `--most-recent-commit` as a convenience instead of `--commit $COMMIT_HASH`.

Do not move to the next task until the current task's verifiable result passes, its commit is made (or the task is `no_commit`), and it is marked complete.

#### For amendments

1. Read all threads with status `needs_attention` across all tasks using `ticket get --json <id>`.
2. For each thread: understand the request, make the code change, run the `verifiable_result` check, stage the changes with `git add`.
3. Create a fixup commit per task: `git commit --fixup=<task_commit_hash>` where `task_commit_hash` is the `commit_hash` field from the task. If a task has no `commit_hash`, create a normal commit with message `<ticket-id> <task-id>: address review — <short summary>` instead.
4. After all fixup commits are staged, run:
   ```bash
   GIT_SEQUENCE_EDITOR=true git rebase -i --autosquash <base_branch>
   ```
   where `base_branch` is `main` (or the repo default branch).
5. After the rebase, each amended task's commit hash has changed. For each amended task, find the new hash:
   ```bash
   git log --reverse --format="%H %s" main..<feature_branch>
   ```
   Match the line whose subject starts with `<ticket-id> <task-id>:`. Then update the stored hash:
   ```bash
   ticket task set-commit <task-id> <new-hash>
   ```
6. Force-push:
   ```bash
   git push --force-with-lease origin <feature_branch>
   ```
7. For each addressed thread: reply with the description of what changed and flip back to open:
   ```bash
   ticket thread reply <thread-id> agent:claude '<description of what was changed>'
   ticket thread transition <thread-id> open
   ```

### 3. Add a note (if warranted)

If you made any non-obvious decisions — a constraint, a tradeoff, a workaround — record them:

```bash
ticket note add <id> agent:claude '<note text>'
```

Skip this step if there is nothing non-obvious to say.

### 4. Hand off for review

```bash
ticket in-review <id>
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
ticket approve <id>
```

### ticket merge

Fast-forward merges the feature branch into main, deletes the branch, removes the worktree, transitions to `merged`.

```bash
ticket merge <id>
```

Preconditions: ticket is `approved`, all tasks complete, no open threads, `feature_branch` and `repo_path` are set. If the branch has diverged, the command errors — rebase manually then retry.

### ticket config

```bash
ticket config set <key> <value>
ticket config get <key>
```

Key `worktrees` (default `true`) controls whether promoting a ticket automatically creates a git worktree at `.worktrees/<ticket-id>/`.
