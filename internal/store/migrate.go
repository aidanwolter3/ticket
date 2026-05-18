package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/aidanwolter/ticket/internal/ids"
)

// runMigrations applies any pending schema/data migrations.
func (s *Store) runMigrations() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version  INTEGER PRIMARY KEY,
		applied  INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var current int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read migration version: %w", err)
	}
	if current < 1 {
		if err := s.migration1(); err != nil {
			return fmt.Errorf("migration 1: %w", err)
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations (version, applied) VALUES (1, ?)`, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("record migration 1: %w", err)
		}
	}
	if current < 2 {
		if err := s.migration2(); err != nil {
			return fmt.Errorf("migration 2: %w", err)
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations (version, applied) VALUES (2, ?)`, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("record migration 2: %w", err)
		}
	}
	if current < 3 {
		if err := s.migration3(); err != nil {
			return fmt.Errorf("migration 3: %w", err)
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations (version, applied) VALUES (3, ?)`, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("record migration 3: %w", err)
		}
	}
	if current < 4 {
		if err := s.migration4(); err != nil {
			return fmt.Errorf("migration 4: %w", err)
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations (version, applied) VALUES (4, ?)`, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("record migration 4: %w", err)
		}
	}
	if current < 5 {
		if err := s.migration5(); err != nil {
			return fmt.Errorf("migration 5: %w", err)
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations (version, applied) VALUES (5, ?)`, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("record migration 5: %w", err)
		}
	}
	if current < 6 {
		if err := s.migration6(); err != nil {
			return fmt.Errorf("migration 6: %w", err)
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations (version, applied) VALUES (6, ?)`, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("record migration 6: %w", err)
		}
	}
	if current < 7 {
		if err := s.migration7(); err != nil {
			return fmt.Errorf("migration 7: %w", err)
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations (version, applied) VALUES (7, ?)`, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("record migration 7: %w", err)
		}
	}
	if current < 8 {
		if err := s.migration8(); err != nil {
			return fmt.Errorf("migration 8: %w", err)
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations (version, applied) VALUES (8, ?)`, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("record migration 8: %w", err)
		}
	}
	if current < 9 {
		if err := s.migration9(); err != nil {
			return fmt.Errorf("migration 9: %w", err)
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations (version, applied) VALUES (9, ?)`, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("record migration 9: %w", err)
		}
	}
	return nil
}

// migration1 transforms the old schema (plan/ticket types with stack_id, commit_hash
// on tickets; comment_threads keyed by ticket_id) into the new schema (tickets are
// plan-level units; tasks are leaf work items; threads are keyed by task_id).
//
// Order: tasks first (per user spec), then plans→tickets, then standalones.
func (s *Store) migration1() error {
	// Detect whether the old schema is present by checking for the stack_id column.
	oldSchema, err := s.hasColumn("tickets", "stack_id")
	if err != nil {
		return err
	}
	if !oldSchema {
		return nil // fresh DB, nothing to migrate
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// ── Step 1: Create the tasks table if it doesn't exist yet ───────────────
	// Intentionally no FOREIGN KEY here: foreign_keys pragma was not yet enabled
	// at this migration's creation time, so CASCADE would be silently ignored.
	// DeleteTicket uses explicit child-row deletion instead of relying on cascade.
	if _, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
		  id                TEXT PRIMARY KEY,
		  ticket_id         TEXT NOT NULL,
		  title             TEXT NOT NULL,
		  description       TEXT NOT NULL DEFAULT '',
		  position          INTEGER NOT NULL,
		  commit_hash       TEXT,
		  verifiable_result TEXT NOT NULL DEFAULT '',
		  completed_at      INTEGER,
		  created           INTEGER NOT NULL,
		  updated           INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create tasks: %w", err)
	}

	// ── Step 2: Identify child tickets (old type='ticket' that are children of a plan) ──
	// A child ticket appears as a blocker_id for a ticket whose type='plan'.
	childRows, err := tx.Query(`
		SELECT DISTINCT t.id, t.title, t.description,
		       COALESCE(t.feature_branch,''), COALESCE(t.commit_hash,''),
		       COALESCE(t.verifiable_result,''), t.created, t.updated,
		       bb.ticket_id AS parent_plan_id
		FROM tickets t
		JOIN blocked_by bb ON bb.blocker_id = t.id
		JOIN tickets p ON p.id = bb.ticket_id AND p.type = 'plan'
		WHERE t.type = 'ticket'
		ORDER BY bb.ticket_id, t.created`)
	if err != nil {
		return fmt.Errorf("query child tickets: %w", err)
	}

	type childInfo struct {
		oldID            string
		title            string
		description      string
		featureBranch    string
		commitHash       string
		verifiableResult string
		createdMs        int64
		updatedMs        int64
		parentPlanID     string
	}
	var children []childInfo
	for childRows.Next() {
		var c childInfo
		if err = childRows.Scan(&c.oldID, &c.title, &c.description,
			&c.featureBranch, &c.commitHash, &c.verifiableResult,
			&c.createdMs, &c.updatedMs, &c.parentPlanID); err != nil {
			childRows.Close()
			return fmt.Errorf("scan child: %w", err)
		}
		children = append(children, c)
	}
	childRows.Close()
	if err = childRows.Err(); err != nil {
		return err
	}

	// Build a set of child ticket IDs for later use.
	childSet := make(map[string]bool, len(children))
	for _, c := range children {
		childSet[c.oldID] = true
	}

	// ── Step 3: Assign positions per parent plan ─────────────────────────────
	// Children are ordered by created timestamp within each parent.
	posCounter := make(map[string]int) // parentPlanID → next position

	// oldTicketToNewTask maps old ticket ID → new task ID (for thread remapping).
	oldTicketToNewTask := make(map[string]string, len(children))

	taskCounter, err := s.nextTaskCounter(tx)
	if err != nil {
		return fmt.Errorf("task counter: %w", err)
	}

	for _, c := range children {
		pos := posCounter[c.parentPlanID] + 1
		posCounter[c.parentPlanID] = pos

		newTaskID := ids.TaskID(taskCounter)
		taskCounter++

		var completedAt interface{} = nil
		// If the old ticket had a commit_hash, treat it as completed.
		if c.commitHash != "" {
			completedAt = c.updatedMs
		}

		if _, err = tx.Exec(`
			INSERT INTO tasks (id, ticket_id, title, description, position,
			  commit_hash, verifiable_result, completed_at, created, updated)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			newTaskID, c.parentPlanID, c.title, c.description, pos,
			nullStrTx(c.commitHash), c.verifiableResult, completedAt,
			c.createdMs, c.updatedMs,
		); err != nil {
			return fmt.Errorf("insert task for %s: %w", c.oldID, err)
		}
		oldTicketToNewTask[c.oldID] = newTaskID

		// Move notes from child ticket to parent ticket.
		if _, err = tx.Exec(`UPDATE notes SET ticket_id=? WHERE ticket_id=?`,
			c.parentPlanID, c.oldID); err != nil {
			return fmt.Errorf("move notes for %s: %w", c.oldID, err)
		}
	}

	// ── Step 4: Handle standalone tickets (old type='ticket' with no plan parent) ─
	standaloneRows, err := tx.Query(`
		SELECT t.id, t.title, t.description,
		       COALESCE(t.feature_branch,''), COALESCE(t.commit_hash,''),
		       COALESCE(t.verifiable_result,''), t.created, t.updated
		FROM tickets t
		WHERE t.type = 'ticket'
		ORDER BY t.created`)
	if err != nil {
		return fmt.Errorf("query standalone tickets: %w", err)
	}

	type standaloneInfo struct {
		oldID            string
		title            string
		description      string
		featureBranch    string
		commitHash       string
		verifiableResult string
		createdMs        int64
		updatedMs        int64
	}
	var standalones []standaloneInfo
	for standaloneRows.Next() {
		var st standaloneInfo
		if err = standaloneRows.Scan(&st.oldID, &st.title, &st.description,
			&st.featureBranch, &st.commitHash, &st.verifiableResult,
			&st.createdMs, &st.updatedMs); err != nil {
			standaloneRows.Close()
			return fmt.Errorf("scan standalone: %w", err)
		}
		if !childSet[st.oldID] {
			standalones = append(standalones, st)
		}
	}
	standaloneRows.Close()
	if err = standaloneRows.Err(); err != nil {
		return err
	}

	for _, st := range standalones {
		// Standalone old tickets become tickets with a single task.
		newTaskID := ids.TaskID(taskCounter)
		taskCounter++

		var completedAt interface{} = nil
		if st.commitHash != "" {
			completedAt = st.updatedMs
		}

		if _, err = tx.Exec(`
			INSERT INTO tasks (id, ticket_id, title, description, position,
			  commit_hash, verifiable_result, completed_at, created, updated)
			VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?)`,
			newTaskID, st.oldID, st.title, st.description,
			nullStrTx(st.commitHash), st.verifiableResult, completedAt,
			st.createdMs, st.updatedMs,
		); err != nil {
			return fmt.Errorf("insert task for standalone %s: %w", st.oldID, err)
		}
		oldTicketToNewTask[st.oldID] = newTaskID
	}

	// ── Step 5: Migrate comment_threads (ticket_id → task_id) ────────────────
	// Create new table, migrate data, replace old table.
	if _, err = tx.Exec(`
		CREATE TABLE comment_threads_new (
		  id       TEXT PRIMARY KEY,
		  task_id  TEXT NOT NULL,
		  status   TEXT NOT NULL DEFAULT 'active'
		           CHECK(status IN ('active','ready','resolved')),
		  created  INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create comment_threads_new: %w", err)
	}

	threadRows, err := tx.Query(`SELECT id, ticket_id, status, created FROM comment_threads`)
	if err != nil {
		return fmt.Errorf("query threads: %w", err)
	}
	type threadRow struct {
		id       string
		ticketID string
		status   string
		created  int64
	}
	var threadList []threadRow
	for threadRows.Next() {
		var r threadRow
		if err = threadRows.Scan(&r.id, &r.ticketID, &r.status, &r.created); err != nil {
			threadRows.Close()
			return fmt.Errorf("scan thread: %w", err)
		}
		threadList = append(threadList, r)
	}
	threadRows.Close()
	if err = threadRows.Err(); err != nil {
		return err
	}

	for _, r := range threadList {
		taskID, ok := oldTicketToNewTask[r.ticketID]
		if !ok {
			// Thread on a plan (plans don't have threads in old model, but guard anyway).
			// Associate with the plan's first task if available, otherwise skip.
			firstTask, ferr := firstTaskForTicket(tx, r.ticketID)
			if ferr != nil || firstTask == "" {
				continue
			}
			taskID = firstTask
		}
		if _, err = tx.Exec(`
			INSERT INTO comment_threads_new (id, task_id, status, created)
			VALUES (?, ?, ?, ?)`,
			r.id, taskID, r.status, r.created); err != nil {
			return fmt.Errorf("migrate thread %s: %w", r.id, err)
		}
	}

	if _, err = tx.Exec(`DROP TABLE comment_threads`); err != nil {
		return fmt.Errorf("drop old comment_threads: %w", err)
	}
	if _, err = tx.Exec(`ALTER TABLE comment_threads_new RENAME TO comment_threads`); err != nil {
		return fmt.Errorf("rename comment_threads: %w", err)
	}

	// ── Step 6: Update old plan rows → type='ticket' ─────────────────────────
	if _, err = tx.Exec(`UPDATE tickets SET type='ticket' WHERE type='plan'`); err != nil {
		return fmt.Errorf("rename plan type: %w", err)
	}

	// ── Step 7: Recreate tickets table with new schema ────────────────────────
	// SQLite can't DROP COLUMN on older versions, so we recreate.
	if _, err = tx.Exec(`
		CREATE TABLE tickets_new (
		  id             TEXT PRIMARY KEY,
		  title          TEXT NOT NULL,
		  description    TEXT NOT NULL DEFAULT '',
		  type           TEXT NOT NULL DEFAULT 'ticket'
		                 CHECK(type IN ('ticket')),
		  status         TEXT NOT NULL DEFAULT 'draft'
		                 CHECK(status IN ('draft','ready','in_progress','in_review','completed')),
		  feature_branch TEXT NOT NULL DEFAULT '',
		  worktree_path  TEXT,
		  created        INTEGER NOT NULL,
		  updated        INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create tickets_new: %w", err)
	}

	// Copy over plan rows (now type='ticket') and standalone old-ticket rows.
	// Child tickets are NOT copied — they are now tasks.
	if _, err = tx.Exec(`
		INSERT INTO tickets_new (id, title, description, type, status, feature_branch, worktree_path, created, updated)
		SELECT t.id, t.title, t.description, t.type, t.status,
		       COALESCE(t.feature_branch,''), NULL, t.created, t.updated
		FROM tickets t
		WHERE t.type = 'ticket'
		  AND t.id NOT IN (
		    SELECT blocker_id FROM blocked_by
		    JOIN tickets p ON p.id = blocked_by.ticket_id AND p.type = 'ticket'
		    WHERE 0  -- placeholder; child deletion handled below
		  )`); err != nil {
		return fmt.Errorf("copy tickets: %w", err)
	}

	// Actually we need to exclude rows that were child tickets (now tasks).
	// Rebuild: copy all remaining rows from tickets (type='ticket') that are NOT in childSet.
	// Since we already set type='ticket' for plans, and old tickets were type='ticket' too,
	// we distinguish by: tickets whose ID appears in oldTicketToNewTask as a CHILD.
	childIDs := make([]string, 0, len(children))
	for _, c := range children {
		childIDs = append(childIDs, c.oldID)
	}

	// Delete child ticket rows from tickets_new (they should not be tickets in the new model).
	for _, cid := range childIDs {
		if _, err = tx.Exec(`DELETE FROM tickets_new WHERE id=?`, cid); err != nil {
			return fmt.Errorf("remove child %s from tickets_new: %w", cid, err)
		}
	}

	if _, err = tx.Exec(`DROP TABLE tickets`); err != nil {
		return fmt.Errorf("drop old tickets: %w", err)
	}
	if _, err = tx.Exec(`ALTER TABLE tickets_new RENAME TO tickets`); err != nil {
		return fmt.Errorf("rename tickets: %w", err)
	}

	// ── Step 8: Clean up blocked_by ───────────────────────────────────────────
	// Remove entries that referenced old child tickets (now tasks).
	// Also remove entries where ticket_id was a plan (plan-child relationships).
	for _, cid := range childIDs {
		if _, err = tx.Exec(`DELETE FROM blocked_by WHERE ticket_id=? OR blocker_id=?`, cid, cid); err != nil {
			return fmt.Errorf("clean blocked_by for %s: %w", cid, err)
		}
	}

	// ── Step 9: Recreate notes FK (notes reference old ticket IDs which are still valid) ─
	// Notes on child tickets were already moved to their parent in step 3.
	// Delete any remaining notes that reference deleted ticket IDs (child IDs).
	for _, cid := range childIDs {
		if _, err = tx.Exec(`DELETE FROM notes WHERE ticket_id=?`, cid); err != nil {
			return fmt.Errorf("clean notes for %s: %w", cid, err)
		}
	}

	// ── Step 10: Add tasks FK (foreign key enforcement needs indexes) ─────────
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_ticket ON tasks(ticket_id)`); err != nil {
		return fmt.Errorf("create tasks index: %w", err)
	}

	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_threads_task ON comment_threads(task_id)`); err != nil {
		return fmt.Errorf("create threads index: %w", err)
	}

	return tx.Commit()
}

// hasTable reports whether the given table exists in the database.
func (s *Store) hasTable(table string) (bool, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// hasColumn reports whether the given table has the given column.
func (s *Store) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// nextTaskCounter returns the next available integer for task ID generation.
func (s *Store) nextTaskCounter(tx *sql.Tx) (int, error) {
	var max sql.NullString
	if err := tx.QueryRow(`SELECT MAX(CAST(SUBSTR(id, 4) AS INTEGER)) FROM tasks`).Scan(&max); err != nil {
		return 1, nil
	}
	n := 1
	if max.Valid {
		fmt.Sscanf(max.String, "%d", &n)
		n++
	}
	return n, nil
}

// firstTaskForTicket returns the id of the first task under a ticket.
func firstTaskForTicket(tx *sql.Tx, ticketID string) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM tasks WHERE ticket_id=? ORDER BY position LIMIT 1`, ticketID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// migration2 adds the config table, adds repo_path column, and recreates the
// tickets table to change the status CHECK (removing 'completed', adding
// 'approved'/'merged'). Existing 'completed' rows are mapped to 'merged'.
func (s *Store) migration2() error {
	// Check outside the transaction — hasColumn uses s.db which would deadlock
	// inside a transaction when MaxOpenConns=1.
	ticketsExists, err := s.hasTable("tickets")
	if err != nil {
		return err
	}
	var hasRepo bool
	if ticketsExists {
		hasRepo, err = s.hasColumn("tickets", "repo_path")
		if err != nil {
			return err
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Add config table.
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS config (
		key    TEXT PRIMARY KEY,
		value  TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create config table: %w", err)
	}

	// Only migrate the tickets table if it already exists; fresh DBs get the
	// correct schema from schema.go after migrations complete.
	if ticketsExists {
		// Add repo_path column if not present.
		if !hasRepo {
			if _, err = tx.Exec(`ALTER TABLE tickets ADD COLUMN repo_path TEXT`); err != nil {
				return fmt.Errorf("add repo_path column: %w", err)
			}
		}

		// Recreate tickets table to change the CHECK constraint.
		// SQLite does not support modifying CHECK constraints in place.
		if _, err = tx.Exec(`
			CREATE TABLE tickets_new (
			  id             TEXT PRIMARY KEY,
			  title          TEXT NOT NULL,
			  description    TEXT NOT NULL DEFAULT '',
			  type           TEXT NOT NULL DEFAULT 'ticket'
			                 CHECK(type IN ('ticket')),
			  status         TEXT NOT NULL DEFAULT 'draft'
			                 CHECK(status IN ('draft','ready','in_progress','in_review','approved','merged')),
			  feature_branch TEXT NOT NULL DEFAULT '',
			  worktree_path  TEXT,
			  repo_path      TEXT,
			  created        INTEGER NOT NULL,
			  updated        INTEGER NOT NULL
			)`); err != nil {
			return fmt.Errorf("create tickets_new: %w", err)
		}

		// Copy rows, mapping 'completed' → 'merged'.
		if _, err = tx.Exec(`
			INSERT INTO tickets_new
			  (id, title, description, type, status, feature_branch, worktree_path, repo_path, created, updated)
			SELECT
			  id, title, description, type,
			  CASE WHEN status = 'completed' THEN 'merged' ELSE status END,
			  feature_branch, worktree_path, repo_path,
			  created, updated
			FROM tickets`); err != nil {
			return fmt.Errorf("copy tickets: %w", err)
		}

		if _, err = tx.Exec(`DROP TABLE tickets`); err != nil {
			return fmt.Errorf("drop old tickets: %w", err)
		}
		if _, err = tx.Exec(`ALTER TABLE tickets_new RENAME TO tickets`); err != nil {
			return fmt.Errorf("rename tickets: %w", err)
		}

		// Recreate indexes dropped with the old table.
		if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status)`); err != nil {
			return fmt.Errorf("recreate idx_tickets_status: %w", err)
		}
		if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tickets_type ON tickets(type)`); err != nil {
			return fmt.Errorf("recreate idx_tickets_type: %w", err)
		}
	}

	return tx.Commit()
}

// migration3 renames thread statuses: active→open, ready→needs_attention.
// Recreates comment_threads with the updated CHECK constraint and maps old values.
func (s *Store) migration3() error {
	threadsExists, err := s.hasTable("comment_threads")
	if err != nil {
		return err
	}
	if !threadsExists {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`
		CREATE TABLE comment_threads_new (
		  id       TEXT PRIMARY KEY,
		  task_id  TEXT NOT NULL,
		  status   TEXT NOT NULL DEFAULT 'open'
		           CHECK(status IN ('open','needs_attention','resolved')),
		  created  INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create comment_threads_new: %w", err)
	}

	if _, err = tx.Exec(`
		INSERT INTO comment_threads_new (id, task_id, status, created)
		SELECT id, task_id,
		  CASE status
		    WHEN 'active' THEN 'open'
		    WHEN 'ready'  THEN 'needs_attention'
		    ELSE status
		  END,
		  created
		FROM comment_threads`); err != nil {
		return fmt.Errorf("migrate thread statuses: %w", err)
	}

	if _, err = tx.Exec(`DROP TABLE comment_threads`); err != nil {
		return fmt.Errorf("drop old comment_threads: %w", err)
	}
	if _, err = tx.Exec(`ALTER TABLE comment_threads_new RENAME TO comment_threads`); err != nil {
		return fmt.Errorf("rename comment_threads: %w", err)
	}

	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_threads_task ON comment_threads(task_id)`); err != nil {
		return fmt.Errorf("recreate idx_threads_task: %w", err)
	}

	return tx.Commit()
}

// migration4 adds the round column (default 1) to the tasks table.
func (s *Store) migration4() error {
	tasksExists, err := s.hasTable("tasks")
	if err != nil {
		return err
	}
	if !tasksExists {
		return nil // fresh DB, schema.go will create the table with round
	}
	hasRound, err := s.hasColumn("tasks", "round")
	if err != nil {
		return err
	}
	if hasRound {
		return nil
	}
	_, err = s.db.Exec(`ALTER TABLE tasks ADD COLUMN round INTEGER NOT NULL DEFAULT 1`)
	return err
}

// migration5 creates the agent_sessions table on existing databases.
// Fresh databases get it from schema.go via CREATE TABLE IF NOT EXISTS.
func (s *Store) migration5() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_sessions (
		  id         TEXT PRIMARY KEY,
		  ticket_id  TEXT NOT NULL,
		  pid        INTEGER NOT NULL,
		  started_at INTEGER NOT NULL,
		  state      TEXT NOT NULL DEFAULT 'running'
		             CHECK(state IN ('running','waiting','terminated','crashed')),
		  log_path   TEXT NOT NULL,
		  FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
		)`)
	if err != nil {
		return fmt.Errorf("create agent_sessions: %w", err)
	}
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_sessions_ticket ON agent_sessions(ticket_id)`)
	return err
}

// migration6 adds file_path and hunk_header columns to comment_threads and draft_threads.
// These columns are nullable and allow threads to be anchored to a specific diff hunk.
func (s *Store) migration6() error {
	type alteration struct {
		table  string
		column string
	}
	for _, a := range []alteration{
		{"comment_threads", "file_path"},
		{"comment_threads", "hunk_header"},
		{"draft_threads", "file_path"},
		{"draft_threads", "hunk_header"},
	} {
		exists, err := s.hasTable(a.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		has, err := s.hasColumn(a.table, a.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := s.db.Exec(`ALTER TABLE ` + a.table + ` ADD COLUMN ` + a.column + ` TEXT`); err != nil {
			return fmt.Errorf("add %s.%s: %w", a.table, a.column, err)
		}
	}
	return nil
}

// migration7 adds the no_commit column (default 0) to the tasks table.
func (s *Store) migration7() error {
	tasksExists, err := s.hasTable("tasks")
	if err != nil {
		return err
	}
	if !tasksExists {
		return nil
	}
	has, err := s.hasColumn("tasks", "no_commit")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	_, err = s.db.Exec(`ALTER TABLE tasks ADD COLUMN no_commit INTEGER NOT NULL DEFAULT 0`)
	return err
}

// migration8 adds the backlog column (default 0) to the tickets table.
func (s *Store) migration8() error {
	ticketsExists, err := s.hasTable("tickets")
	if err != nil {
		return err
	}
	if !ticketsExists {
		return nil
	}
	has, err := s.hasColumn("tickets", "backlog")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	_, err = s.db.Exec(`ALTER TABLE tickets ADD COLUMN backlog INTEGER NOT NULL DEFAULT 0`)
	return err
}

// migration9 adds "preparing" and "tearing_down" to the tickets status CHECK
// constraint by recreating the tickets table (SQLite does not support ALTER
// TABLE … MODIFY CONSTRAINT).
func (s *Store) migration9() error {
	ticketsExists, err := s.hasTable("tickets")
	if err != nil {
		return err
	}
	if !ticketsExists {
		return nil // fresh DB gets correct schema from schema.go
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`
		CREATE TABLE tickets_new (
		  id             TEXT PRIMARY KEY,
		  title          TEXT NOT NULL,
		  description    TEXT NOT NULL DEFAULT '',
		  type           TEXT NOT NULL DEFAULT 'ticket'
		                 CHECK(type IN ('ticket')),
		  status         TEXT NOT NULL DEFAULT 'draft'
		                 CHECK(status IN ('draft','ready','preparing','in_progress','tearing_down','in_review','approved','merged')),
		  feature_branch TEXT NOT NULL DEFAULT '',
		  worktree_path  TEXT,
		  repo_path      TEXT,
		  backlog        INTEGER NOT NULL DEFAULT 0,
		  created        INTEGER NOT NULL,
		  updated        INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create tickets_new: %w", err)
	}

	if _, err = tx.Exec(`
		INSERT INTO tickets_new
		  (id, title, description, type, status, feature_branch, worktree_path, repo_path, backlog, created, updated)
		SELECT id, title, description, type, status, feature_branch, worktree_path, repo_path, backlog, created, updated
		FROM tickets`); err != nil {
		return fmt.Errorf("copy tickets: %w", err)
	}

	if _, err = tx.Exec(`DROP TABLE tickets`); err != nil {
		return fmt.Errorf("drop old tickets: %w", err)
	}
	if _, err = tx.Exec(`ALTER TABLE tickets_new RENAME TO tickets`); err != nil {
		return fmt.Errorf("rename tickets: %w", err)
	}

	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status)`); err != nil {
		return fmt.Errorf("recreate idx_tickets_status: %w", err)
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tickets_type ON tickets(type)`); err != nil {
		return fmt.Errorf("recreate idx_tickets_type: %w", err)
	}

	return tx.Commit()
}

func nullStrTx(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
