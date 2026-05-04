# Ticket Tracker — Design Document

**Status:** Draft v1
**Owner:** Aidan
**Last updated:** 2026-05-03

---

## 1. Motivation

Existing ticket tools (e.g., Beads) are over-engineered: they install git hooks, run daemons, force auto-commits, and are notoriously hard to remove. We want a minimal, locally-stored ticket tracker designed around a specific workflow: a human plans work, agents (Claude Code, Gemini, etc.) pick up ready work and execute it, and the human reviews the results. Multiple agents can work in parallel.

The v1 of this system is a TUI for managing tickets and conducting reviews. Agent invocation happens out-of-band — the TUI is purely the human surface for planning and review.

## 2. Goals

- **Local-first**: SQLite database, single binary, no servers, no cloud.
- **Minimal surface area**: One conceptual entity (the ticket), with a type discriminator for plans/epics later.
- **Generic comment system**: Threads can come from anywhere (self-review, peer review, CI bots) and use the same lifecycle.
- **Stack-aware reviews**: Stacks are always reviewed as a unit, never piecemeal.
- **Agent-friendly data model**: Tickets carry enough context (description, verifiable result, notes, threads) for an agent to pick up work without external coordination.
- **Human-controlled state**: Only humans transition thread states. Agents propose; humans decide.

## 3. Non-goals (v1)

- Agent invocation. The TUI does not spawn or manage agents. Agents read/write the database directly, out of band.
- Git operations. The TUI does not run `git`. It stores commit hashes and branch names as metadata; actual VCS work happens elsewhere.
- Verification execution. The TUI stores the verification recipe as text; it does not run tests.
- Multi-user / network sync. Single-user local DB only.
- Notifications, deadlines, time tracking, labels, milestones (beyond what `type` allows).

## 4. Data model

### 4.1 Conceptual model

```
Ticket {
  id, title, description, type
  status
  featureBranch, stackId, commitHash
  blockedBy: [ticket_ids]
  verifiableResult: string (markdown)
  commentThreads: [Thread]
  notes: [Note]
  created, updated
}

Thread {
  id, status
  messages: [Message]
}

Message {
  author, text, created
}

Note {
  author, text, created
}
```

### 4.2 SQLite schema

```sql
PRAGMA foreign_keys = ON;

CREATE TABLE tickets (
  id              TEXT PRIMARY KEY,            -- e.g., "T-042"
  title           TEXT NOT NULL,
  description     TEXT NOT NULL DEFAULT '',
  type            TEXT NOT NULL DEFAULT 'ticket'
                  CHECK(type IN ('ticket', 'plan')),
  status          TEXT NOT NULL DEFAULT 'draft'
                  CHECK(status IN ('draft','ready','in_progress','in_review','completed')),
  feature_branch  TEXT NOT NULL DEFAULT '',
  stack_id        TEXT,                        -- NULL for non-stacked
  commit_hash     TEXT,                        -- NULL until work is committed
  verifiable_result TEXT NOT NULL DEFAULT '',
  created         INTEGER NOT NULL,            -- unix epoch ms
  updated         INTEGER NOT NULL
);

CREATE TABLE blocked_by (
  ticket_id   TEXT NOT NULL,
  blocker_id  TEXT NOT NULL,
  PRIMARY KEY (ticket_id, blocker_id),
  FOREIGN KEY (ticket_id)  REFERENCES tickets(id) ON DELETE CASCADE,
  FOREIGN KEY (blocker_id) REFERENCES tickets(id) ON DELETE CASCADE,
  CHECK (ticket_id != blocker_id)
);

CREATE TABLE comment_threads (
  id          TEXT PRIMARY KEY,
  ticket_id   TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'active'
              CHECK(status IN ('active','ready','resolved')),
  created     INTEGER NOT NULL,
  FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
);

CREATE TABLE thread_messages (
  id          TEXT PRIMARY KEY,
  thread_id   TEXT NOT NULL,
  author      TEXT NOT NULL,    -- e.g., "human:aidan", "agent:claude-1"
  text        TEXT NOT NULL,
  created     INTEGER NOT NULL,
  FOREIGN KEY (thread_id) REFERENCES comment_threads(id) ON DELETE CASCADE
);

CREATE TABLE notes (
  id          TEXT PRIMARY KEY,
  ticket_id   TEXT NOT NULL,
  author      TEXT NOT NULL,
  text        TEXT NOT NULL,
  created     INTEGER NOT NULL,
  FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
);

CREATE INDEX idx_tickets_status        ON tickets(status);
CREATE INDEX idx_tickets_type          ON tickets(type);
CREATE INDEX idx_tickets_stack         ON tickets(stack_id);
CREATE INDEX idx_blocked_by_blocker    ON blocked_by(blocker_id);
CREATE INDEX idx_threads_ticket        ON comment_threads(ticket_id);
CREATE INDEX idx_thread_messages_thread ON thread_messages(thread_id);
CREATE INDEX idx_notes_ticket          ON notes(ticket_id);
```

### 4.3 Field semantics

| Field | Notes |
|---|---|
| `id` | Human-readable, e.g., `T-042`. Generated as `T-` + monotonically increasing integer. |
| `type` | `ticket` or `plan`. Future: `epic`. Discriminator for hierarchy. |
| `status` | See state machine in §5.1. |
| `feature_branch` | Free text. The branch the work lives on. Empty for plans typically. |
| `stack_id` | Free text. Tickets sharing a `stack_id` form a stack on a single branch. |
| `commit_hash` | Set by agent when work is pushed; cleared if work is reset. |
| `verifiable_result` | Markdown. Human-readable instructions describing how to verify "done." |
| `blocked_by` | Many-to-many. Plans use this to indicate child tickets. |

### 4.4 Stack ordering

Stack position is **derived from commit order on the feature branch**, not stored. To compute order:
1. Read all tickets where `stack_id = X`.
2. Their `commit_hash` values define the order via `git log` on the feature branch.
3. Tickets without a commit yet are unordered (typically `draft` or `ready`).

For v1, since the TUI doesn't run git, ordering can be ticket creation order as a fallback when commits are missing. Agents that touch git will be responsible for keeping commits aligned with ticket order.

## 5. State machines

### 5.1 Ticket status

```
       ┌───────┐
       │ draft │
       └───┬───┘
           │ human marks ready
           ▼
       ┌───────┐
       │ ready │ ◀─────────────────────┐
       └───┬───┘                       │
           │ agent picks up            │
           ▼                           │
       ┌─────────────┐                 │
       │ in_progress │                 │
       └──────┬──────┘                 │
              │ agent finishes work    │
              ▼                        │
       ┌────────────┐                  │ human transitions back
       │ in_review  │ ─────────────────┘ when amendments needed
       └──────┬─────┘
              │ human marks completed
              ▼
       ┌───────────┐
       │ completed │
       └───────────┘
```

**Transitions:**

| From | To | Trigger |
|---|---|---|
| `draft` | `ready` | Human |
| `ready` | `draft` | Human (changed mind) |
| `ready` | `in_progress` | Agent (when picking up work) |
| `in_progress` | `in_review` | Agent (when work is committed) |
| `in_review` | `ready` | Human (amendments needed) |
| `in_review` | `completed` | Human (approves) |
| any → `draft` | Human (manual override) |

**Notes on `ready` semantics:**
- A `ready` ticket with **no commit** = fresh implementation work.
- A `ready` ticket **with a commit and `ready`-status threads** = amendment work.
- The agent infers the task from the ticket state.

### 5.2 Thread status

```
   ┌────────┐
   │ active │ ◀─────────────────┐
   └────┬───┘                   │
        │ human marks ready     │
        ▼                       │
   ┌───────┐                    │
   │ ready │                    │
   └───┬───┘                    │
       │ agent posts amendment  │
       │ reply (auto)           │
       └───────────────────────►┤
       │                        │
       │ human resolves         │
       ▼                        │
   ┌──────────┐                 │
   │ resolved │                 │
   └──────────┘                 │
                                │
   (human can also move ready ──┘
    back to active manually)
```

**Transitions:**

| From | To | Trigger |
|---|---|---|
| `active` | `ready` | Human only |
| `ready` | `active` | Human OR agent (when agent posts amendment) |
| `active` | `resolved` | Human only |
| `ready` | `resolved` | Human only (effectively dismisses without amendment) |
| `resolved` | `active` | Human only (re-opens) |

**Key rule:** Only humans transition threads to `resolved`. Agents only flip `ready → active` after posting a reply.

### 5.3 Ticket-thread coupling

| Ticket status | Thread behavior |
|---|---|
| `draft`, `ready`, `in_progress` | Threads typically not present. If they exist, agents ignore them until ticket is `in_review`. |
| `in_review` | Humans add/edit threads. Agents ignore. |
| `in_review` → `ready` | Human is signaling "amend ready threads." Agent picks up. |
| `completed` | All threads should be `resolved`. UI warns if not. |

## 6. Workflow

### 6.1 The full lifecycle (annotated example)

Aidan wants to redesign the auth system.

**Phase 1: Planning (out of band)**

Aidan creates a plan + tickets via the TUI:

```
T-042 [plan]   "Auth redesign"
  blocked_by: [T-043, T-044, T-045]
  status: draft

T-043 [ticket] "Add JWT validation"
  feature_branch: feat/auth-argon2
  stack_id: auth-1
  status: draft
  verifiable_result: "Run `npm test -- auth/jwt`. All tests pass."

T-044 [ticket] "Replace bcrypt with argon2"
  feature_branch: feat/auth-argon2
  stack_id: auth-1
  blocked_by: [T-043]
  status: draft
  verifiable_result: "Run `npm test -- auth/`. All tests pass. Login works with both old and new hashes."

T-045 [ticket] "Migrate user sessions"
  feature_branch: feat/auth-argon2
  stack_id: auth-1
  blocked_by: [T-044]
  status: draft
  verifiable_result: "Run `npm test -- auth/sessions`. All sessions migrate without forced logouts."
```

Aidan reviews the plan, decides it's solid, marks tickets as `ready` (or leaves them `draft` to defer).

**Phase 2: Implementation (out of band — agent reads DB)**

An agent (running externally, e.g., Claude Code) queries:

```sql
SELECT t.* FROM tickets t
WHERE t.status = 'ready'
  AND t.type = 'ticket'
  AND NOT EXISTS (
    SELECT 1 FROM blocked_by b
    JOIN tickets bt ON bt.id = b.blocker_id
    WHERE b.ticket_id = t.id AND bt.status != 'completed'
  );
```

This returns T-043 (T-044 and T-045 are blocked).

The agent:
1. Updates T-043 to `in_progress`.
2. Reads description, verifiable_result, notes.
3. Implements the work, commits to `feat/auth-argon2`.
4. Adds notes for future agents: `"JWT lib uses RS256 by default; we use HS256 here"`.
5. Updates T-043 with `commit_hash`, status `in_review`.

T-044 is now unblocked. A different agent picks it up. Same flow.

**Phase 3: Review**

Aidan opens the TUI, switches to "Review Queue" tab. He sees:

```
Stacks ready for review — 1 stack
  auth-1   Auth redesign
           3 tickets · 0 threads (fresh)
           T-043 ◐  T-044 ◐  T-045 ◐
```

Aidan reviews the entire stack. He leaves comments:
- On T-044: thread "salt parameter is hardcoded" (status: `active`)
- On T-045: thread "missing migration logging" (status: `active`)

After thinking, he marks both threads `ready`. He moves T-044 and T-045 back to `ready` status (T-043 stays `in_review` since it has no comments — Aidan has not yet approved it but isn't requesting amendments either).

**Phase 4: Amendment**

A single agent picks up the stack (since multiple stacked tickets are `ready` together):
1. Reads T-044 + its `ready` threads + child T-045 + its `ready` threads.
2. Amends the bottom commit (T-044), rebases T-045 on top.
3. For each amended thread, posts a reply: `"Updated to use SALT_LENGTH constant in src/auth/config.ts"`. Thread auto-transitions back to `active`.
4. Runs each ticket's `verifiable_result`. If it fails, fixes and re-runs.
5. Pushes amended stack, updates `commit_hash` on each ticket.
6. Sets T-044 and T-045 back to `in_review`.

**Phase 5: Approval**

Aidan reviews the amendments. He's happy:
- Marks all threads `resolved`.
- Marks T-043, T-044, T-045 as `completed`.
- T-042 (the plan) is now unblocked. Aidan marks it `completed` too.

### 6.2 Peer review variant

Same as above through Phase 5, except:
- After Aidan's self-review, he doesn't mark tickets `completed`.
- An external tool (or Aidan manually) inserts threads from `human:alice` into the database.
- Those threads start as `active`. Aidan triages them:
  - Some he marks `resolved` directly (dismissing).
  - Some he replies to, then marks `ready` for amendment.
- Loop returns to Phase 4.

The system is symmetric: it doesn't care who authored a comment. Aidan controls the gates.

### 6.3 Standalone tickets

Tickets without a `stack_id` (e.g., T-046 "Audit log refactor") follow the same lifecycle but are reviewed individually rather than as a stack. They appear in the "Standalone tickets" section of the Review Queue.

### 6.4 Plans

A plan is a ticket with `type='plan'`. Plans:
- Use `blocked_by` to reference child tickets.
- Are typically completed only after all children are completed.
- Can have their own description, notes, and threads (e.g., for cross-cutting design discussion).
- May or may not have a `feature_branch` or `verifiable_result` — typically empty.

## 7. TUI specification

### 7.1 Tabs (top-level navigation)

- **Tickets**: All tickets, plans-first hierarchy.
- **Review Queue**: Subset of tickets needing human attention.

Switch tabs with `tab` / `shift+tab` or numeric shortcuts (`1`, `2`).

### 7.2 Tickets tab

**Layout:**
- Filter row: status filter, type filter (optional), search.
- List body: plans rendered first (cyan), each expandable to show child tickets indented underneath. Standalone tickets in a separate section at the bottom.
- Status icons: `◌ draft`, `○ ready`, `● in_progress`, `◐ in_review`, `✓ completed`.

**Keys:**
- `↑↓` navigate
- `space` expand/collapse a plan
- `enter` open ticket detail
- `n` new ticket
- `/` search
- `f` cycle filter
- `s` jump to stack view (if ticket has a stack)
- `q` quit

**Default state:** Plans expanded, filter shows all non-`completed`.

### 7.3 Ticket detail view

Sections:
- Header (id, title, type, status, branch, stack, commit)
- Description
- Verifiable Result (markdown rendered if simple, otherwise plain text)
- Blocked By (with status of each blocker)
- Threads (collapsible, count summary)
- Notes (collapsible, count summary)

**Keys:**
- `e` edit (opens form)
- `t` jump to threads view
- `n` add note
- `b` edit blocked_by
- `s` change status
- `esc` back

### 7.4 Threads view

Each thread renders with:
- Status icon (`● active`, `◆ ready`, `✓ resolved`)
- One-line summary (first message truncated)
- Message count
- Expandable to show full conversation

**Per-thread keys (when expanded):**
- `r` reply (opens editor for new message)
- `→` change status: `active → ready` or `ready → active`
- `✓` mark `resolved`
- `←` (when applicable) move `resolved` back to `active`

**View-level keys:**
- `n` new thread
- `↑↓` navigate
- `enter` expand/collapse
- `esc` back

### 7.5 Stack view

Lists all tickets in a stack in commit order, with current ticket highlighted. Shows stack health summary.

**Keys:**
- `enter` open ticket
- `r` review all (walk every ticket sequentially)
- `esc` back

### 7.6 Plan detail view

Same template as ticket detail, plus:
- Children list (rendered from `blocked_by`) with status icons
- Progress bar (% of children completed)

**Keys:**
- `a` add child (creates a ticket with this plan in its `blocked_by`)
- Other keys same as ticket detail

### 7.7 Review Queue tab

**Two sections:**

1. **Stacks ready for review** — only shown when **all** tickets in the stack are `in_review`. Each entry shows:
   - Stack ID and parent plan title (if any)
   - Summary line: `N tickets · M active threads · K ready`
   - Per-ticket inline status with thread counts

2. **Standalone tickets** — `in_review` tickets with no `stack_id`:
   - Title, active thread count

**Keys:**
- `↑↓` navigate
- `enter` open ticket (or first ticket of stack)
- `r` review (for stacks, walks the stack; for standalone, opens ticket)
- `tab` switch to Tickets tab
- `q` quit

**Empty state:** "No tickets pending review."

### 7.8 New / Edit forms

A modal form with fields:
- Title (required)
- Type (radio: ticket / plan)
- Description (multiline)
- Feature branch (free text)
- Stack ID (free text, optional)
- Verifiable result (multiline markdown)
- Blocked by (comma-separated IDs)
- Status (radio, defaults to `draft`)

**Keys:**
- `tab` next field
- `shift+tab` previous field
- `enter` (in single-line fields) advance; (in multiline) newline
- `ctrl+s` save
- `esc` cancel

### 7.9 Color and style

- Cyan: plans
- White: tickets
- Yellow: `in_review`
- Green: `completed`
- Red dim: `blocked` (when blocker isn't completed)
- Magenta: stacks
- Default: everything else

Use a single Bubble Tea Lipgloss theme defined in `internal/tui/style.go`.

## 8. Tech stack

| Concern | Choice | Rationale |
|---|---|---|
| Language | Go 1.22+ | Single static binary, fast, good TUI ecosystem |
| TUI framework | [Bubble Tea](https://github.com/charmbracelet/bubbletea) | Modern, well-maintained, declarative |
| Styling | [Lipgloss](https://github.com/charmbracelet/lipgloss) | Pairs with Bubble Tea |
| Components | [Bubbles](https://github.com/charmbracelet/bubbles) | List, textinput, viewport, etc. |
| Database | SQLite via [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) | Pure Go driver, no CGo, easier cross-compilation |
| Migrations | [`golang-migrate`](https://github.com/golang-migrate/migrate) or hand-rolled | Hand-rolled is fine for v1 |
| ID generation | Monotonic counter in DB (`SELECT MAX(id)+1`) | Simple, human-readable IDs |
| Time | `time.Now().UnixMilli()` stored as INTEGER | Sortable, timezone-agnostic |
| Testing | Standard `testing` + [`testify`](https://github.com/stretchr/testify) | Idiomatic |

## 9. Project structure

```
ticket/
├── cmd/
│   └── ticket/
│       └── main.go             # Entry point: parses flags, opens DB, starts TUI
├── internal/
│   ├── store/
│   │   ├── schema.go           # Embedded SQL migrations
│   │   ├── store.go            # Connection, migration runner
│   │   ├── tickets.go          # CRUD for tickets
│   │   ├── threads.go          # CRUD for threads + messages
│   │   ├── notes.go            # CRUD for notes
│   │   ├── queries.go          # Higher-level queries (review queue, stacks, available work)
│   │   └── store_test.go
│   ├── model/
│   │   ├── ticket.go           # Ticket, Status, Type
│   │   ├── thread.go           # Thread, Message, ThreadStatus
│   │   ├── note.go             # Note
│   │   └── transitions.go      # State machine validators
│   ├── tui/
│   │   ├── app.go              # Top-level Bubble Tea model
│   │   ├── tabs.go             # Tab switcher
│   │   ├── views/
│   │   │   ├── tickets.go      # Tickets tab
│   │   │   ├── review_queue.go # Review Queue tab
│   │   │   ├── ticket_detail.go
│   │   │   ├── threads.go
│   │   │   ├── stack.go
│   │   │   ├── plan_detail.go
│   │   │   └── form.go         # Shared new/edit form
│   │   ├── components/
│   │   │   ├── status_icon.go
│   │   │   ├── thread_view.go
│   │   │   └── progress_bar.go
│   │   ├── keys.go             # Keybinding definitions
│   │   └── style.go            # Lipgloss theme
│   └── ids/
│       └── ids.go              # Generate "T-042"-style IDs
├── go.mod
├── go.sum
├── README.md
└── design.md                   # This document
```

## 10. Testable milestones

Each milestone produces a runnable, testable artifact. Don't move on until tests pass.

### Milestone 1 — Schema and store layer

**Deliverable:** `internal/store` with migration support and CRUD for all entities.

**Tests:**
1. Open a fresh DB, run migrations, verify all tables/indexes exist.
2. Create a ticket, retrieve it, fields match.
3. Create a plan with three children (via `blocked_by`), retrieve children for plan.
4. Create thread, add messages, retrieve in order.
5. Add notes, retrieve in order.
6. Cascade delete: deleting a ticket removes its threads, messages, notes, and `blocked_by` entries.
7. Status transition validation: invalid transition (e.g., `draft → completed`) returns error.
8. Thread status transition validation: agent attempting `active → resolved` returns error.

### Milestone 2 — Higher-level queries

**Deliverable:** `internal/store/queries.go` with the queries the TUI needs.

**Tests:**
1. `AvailableWork()` returns tickets that are `ready` AND have all blockers `completed`. Plans are excluded.
2. `ReviewQueue()` returns:
   - All stacks where every ticket in the stack is `in_review`
   - All standalone (no `stack_id`) tickets in `in_review`
3. Stacks with mixed statuses are NOT in the review queue.
4. `TicketHierarchy()` returns plans first with their children, then standalone tickets.
5. `BlockingTickets(id)` returns tickets that have `id` in their `blocked_by`.

### Milestone 3 — Top-level TUI scaffold

**Deliverable:** A binary that launches, shows the Tickets tab with seed data, and lets you switch tabs.

**Tests:**
1. Manual: launch with empty DB, see "No tickets" state.
2. Manual: seed DB with sample data, see tickets listed.
3. Manual: `tab` cycles between Tickets and Review Queue.
4. Manual: `q` quits cleanly.
5. Unit: TUI model state transitions on keypress events.

### Milestone 4 — Tickets tab (read-only)

**Deliverable:** Tickets tab fully functional in read mode.

**Tests:**
1. Manual: plans render first in cyan, expanded by default.
2. Manual: `space` collapses/expands a plan.
3. Manual: standalone tickets appear at the bottom.
4. Manual: filter cycles correctly through statuses.
5. Manual: `enter` opens ticket detail; `esc` returns.

### Milestone 5 — Ticket creation and editing

**Deliverable:** New/edit forms; full CRUD from the TUI.

**Tests:**
1. Manual: `n` opens new ticket form; saving creates ticket, returns to list.
2. Manual: `e` opens edit form pre-populated; saving updates fields.
3. Manual: `b` opens blocked_by editor.
4. Manual: status changes via `s` are validated (invalid transitions show error).
5. Unit: form validation rejects empty title.

### Milestone 6 — Threads and notes

**Deliverable:** Threads view fully functional. Notes can be added/viewed.

**Tests:**
1. Manual: open ticket, press `t`, see threads.
2. Manual: `n` (in threads view) creates new thread; first message editor opens.
3. Manual: `r` adds reply to selected thread.
4. Manual: `→` toggles `active ↔ ready`; `✓` marks resolved.
5. Manual: notes added via `n` (in detail view) appear in notes section.
6. Unit: thread state transitions validated (humans only for resolved).

### Milestone 7 — Review Queue tab

**Deliverable:** Review Queue fully functional.

**Tests:**
1. Manual: stack with all tickets `in_review` appears under "Stacks."
2. Manual: stack with mixed statuses does NOT appear.
3. Manual: standalone `in_review` ticket appears under "Standalone."
4. Manual: `enter` opens first ticket of a stack; `r` walks the stack.
5. Manual: empty queue shows "No tickets pending review."

### Milestone 8 — Stack view and plan detail

**Deliverable:** Stack view + plan detail fully functional.

**Tests:**
1. Manual: stack view lists all tickets in commit order.
2. Manual: plan detail shows progress bar, children list with statuses.
3. Manual: `a` in plan detail creates a child ticket linked via `blocked_by`.
4. Manual: completing all children unblocks the plan visually.

### Milestone 9 — Polish

**Deliverable:** Theme consistency, error handling, help screen.

**Tests:**
1. Manual: `?` opens help overlay listing keybindings.
2. Manual: DB errors render as bottom status messages, not crashes.
3. Manual: terminal resize handled gracefully.
4. Manual: colors render correctly in light and dark terminals.

## 11. Examples / scenarios

### 11.1 Querying available work for an agent

```sql
SELECT t.*
FROM tickets t
WHERE t.status = 'ready'
  AND t.type = 'ticket'
  AND NOT EXISTS (
    SELECT 1
    FROM blocked_by b
    JOIN tickets blocker ON blocker.id = b.blocker_id
    WHERE b.ticket_id = t.id AND blocker.status != 'completed'
  )
ORDER BY t.created ASC;
```

### 11.2 Review queue: full stacks

```sql
WITH stack_status AS (
  SELECT
    stack_id,
    COUNT(*) AS total,
    SUM(CASE WHEN status = 'in_review' THEN 1 ELSE 0 END) AS in_review_count
  FROM tickets
  WHERE stack_id IS NOT NULL
  GROUP BY stack_id
)
SELECT t.*
FROM tickets t
JOIN stack_status s ON s.stack_id = t.stack_id
WHERE s.total = s.in_review_count
ORDER BY t.stack_id, t.created;
```

### 11.3 Standalone in-review tickets

```sql
SELECT * FROM tickets
WHERE status = 'in_review' AND stack_id IS NULL;
```

### 11.4 Active threads on a ticket

```sql
SELECT t.*, COUNT(m.id) AS message_count
FROM comment_threads t
LEFT JOIN thread_messages m ON m.thread_id = t.id
WHERE t.ticket_id = ?
GROUP BY t.id
ORDER BY t.created;
```

### 11.5 Sample agent code (pseudocode, NOT implemented in v1)

```python
# This is what an external agent might do. Not part of this codebase.
import sqlite3

conn = sqlite3.connect("tickets.db")
ticket = conn.execute(AVAILABLE_WORK_QUERY).fetchone()
if not ticket:
    sys.exit(0)

# Claim it
conn.execute("UPDATE tickets SET status='in_progress', updated=? WHERE id=?",
             (now_ms(), ticket["id"]))

# ... do the work, commit code ...

# Update with results
conn.execute("""
  UPDATE tickets
  SET status='in_review', commit_hash=?, updated=?
  WHERE id=?
""", (commit_hash, now_ms(), ticket["id"]))

# Add notes for future agents
conn.execute("INSERT INTO notes (id, ticket_id, author, text, created) VALUES (?,?,?,?,?)",
             (uuid(), ticket["id"], "agent:claude-1", "JWT lib uses HS256 here", now_ms()))
```

## 12. Open questions / future work

- **Agent invocation**: How to spawn agents from the TUI (later milestone).
- **Verification execution**: Should the TUI shell out to run `verifiable_result`? Or strictly out-of-band?
- **Multi-user / sync**: Probably never, but if so: which sync protocol?
- **Search**: Full-text search across descriptions and threads (FTS5 in SQLite).
- **Templates**: Common ticket templates (bug, feature, refactor) to speed up creation.
- **Bulk operations**: e.g., "mark all threads on this stack `ready`".
- **History / audit log**: Currently nothing tracks status transitions over time.
- **Markdown rendering**: Render descriptions and verifiable_result as styled markdown in the terminal (Glamour).

## 13. Implementation notes for the coding agent

The agent writing this code should know:

1. **The DB file path** is configurable via `--db` flag, defaults to `~/.local/share/ticket/tickets.db`. Create the directory if it doesn't exist.
2. **Migrations** run on every startup. Schema version is tracked in a `schema_migrations` table. Migrations are forward-only.
3. **All status transitions** should go through `model/transitions.go` so validation is centralized. The store layer should reject invalid transitions at write time.
4. **Time** is always stored as `time.Now().UnixMilli()` in INTEGER columns. Display layer formats it.
5. **IDs**:
   - Tickets use the form `T-NNN` where `NNN` is a zero-padded incrementing integer (3+ digits). Generate via a counter table or `SELECT MAX(...)`.
   - Threads, messages, and notes use UUIDs (no need for human-readable IDs).
6. **The TUI** must NOT block on DB writes. Use Bubble Tea's `tea.Cmd` pattern for I/O.
7. **The TUI** must handle terminal resize (`tea.WindowSizeMsg`).
8. **Error handling**: never panic in the TUI. Surface errors as a bottom-of-screen status message.
9. **Empty states**: every list view must handle the empty case gracefully with a helpful message.
10. **Testing**:
    - Store layer: standard table-driven tests with a temp DB per test.
    - TUI: test the `Update` function with synthesized `tea.Msg` events; assert resulting model state.
11. **No global state**. Pass the store explicitly into the TUI model.
12. **Logging**: use `log/slog` to a file (`~/.local/share/ticket/ticket.log`), not stdout (it would corrupt the TUI).
13. **Concurrent agent access**: SQLite handles this with WAL mode. Enable it on connection: `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`.
14. **Dependencies**: use the latest stable versions of Bubble Tea, Lipgloss, Bubbles, and `modernc.org/sqlite`. Pin versions in `go.mod`.
15. **Cross-platform**: must build cleanly on macOS and Linux. Windows support is a stretch goal (Bubble Tea works on Windows but some terminals are quirky).
16. **Don't over-engineer**: this is v1. Resist the urge to add layered abstractions, plugin systems, or premature optimization. Read the schema → render the data → handle the keypresses. Keep it simple.

## 14. Acceptance criteria for v1

The following must all be true for v1 to ship:

- [ ] All milestones 1-9 complete and tested.
- [ ] Single binary builds on Go 1.22+.
- [ ] Binary launches in under 100ms with a populated DB (~1000 tickets).
- [ ] No CGo dependencies (so we can cross-compile easily).
- [ ] README explains: how to install, how to run, where the DB lives, basic keybindings.
- [ ] Help overlay (`?`) lists all keybindings in the current view.
- [ ] At least 70% test coverage in `internal/store` and `internal/model`.
- [ ] No data loss bugs: every observed crash should leave the DB in a consistent state (SQLite + WAL handles this).

---

**End of design document.**
