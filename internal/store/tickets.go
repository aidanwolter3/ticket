package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aidanwolter/ticket/internal/ids"
	"github.com/aidanwolter/ticket/internal/model"
)

func (s *Store) nextTicketID() (string, error) {
	var max sql.NullString
	err := s.db.QueryRow(`SELECT MAX(CAST(SUBSTR(id, 3) AS INTEGER)) FROM tickets`).Scan(&max)
	if err != nil {
		return "", err
	}
	n := 1
	if max.Valid {
		fmt.Sscanf(max.String, "%d", &n)
		n++
	}
	return ids.TicketID(n), nil
}

func (s *Store) CreateTicket(t *model.Ticket) error {
	if t.FeatureBranch != "" {
		return fmt.Errorf("feature_branch must not be set on ticket creation; it is assigned automatically when work is dispatched")
	}
	id, err := s.nextTicketID()
	if err != nil {
		return fmt.Errorf("generate id: %w", err)
	}
	t.ID = id
	now := time.Now().UnixMilli()
	t.Created = time.UnixMilli(now)
	t.Updated = time.UnixMilli(now)

	_, err = s.db.Exec(`
		INSERT INTO tickets (id, title, description, type, status, feature_branch, worktree_path, repo_path, created, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Title, t.Description, string(t.Type), string(t.Status),
		t.FeatureBranch, nullStr(t.WorktreePath), nullStr(t.RepoPath), now, now,
	)
	if err != nil {
		return fmt.Errorf("insert ticket: %w", err)
	}
	return s.setBlockedBy(t.ID, t.BlockedBy)
}

func (s *Store) UpdateTicket(t *model.Ticket) error {
	now := time.Now().UnixMilli()
	t.Updated = time.UnixMilli(now)
	_, err := s.db.Exec(`
		UPDATE tickets SET title=?, description=?, type=?, status=?, feature_branch=?,
		  worktree_path=?, repo_path=?, updated=?
		WHERE id=?`,
		t.Title, t.Description, string(t.Type), string(t.Status),
		t.FeatureBranch, nullStr(t.WorktreePath), nullStr(t.RepoPath), now, t.ID,
	)
	if err != nil {
		return fmt.Errorf("update ticket: %w", err)
	}
	return s.setBlockedBy(t.ID, t.BlockedBy)
}

func (s *Store) TransitionTicket(id string, to model.Status) error {
	var fromStr string
	err := s.db.QueryRow(`SELECT status FROM tickets WHERE id=?`, id).Scan(&fromStr)
	if err == sql.ErrNoRows {
		return fmt.Errorf("ticket %s not found", id)
	}
	if err != nil {
		return err
	}
	from := model.Status(fromStr)
	if err := model.ValidateTicketTransition(from, to); err != nil {
		return err
	}
	if err := s.checkTransitionPreconditions(id, from, to); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	_, err = s.db.Exec(`UPDATE tickets SET status=?, updated=? WHERE id=?`, string(to), now, id)
	return err
}

// checkTransitionPreconditions enforces semantic guards that require DB state
// beyond author/status-graph checks.
func (s *Store) checkTransitionPreconditions(id string, from, to model.Status) error {
	switch {
	case from == model.StatusDraft && to == model.StatusReady:
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE ticket_id=?`, id).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("ticket %s has no tasks: add at least one task before marking ready", id)
		}

	case (from == model.StatusInProgress && to == model.StatusInReview) ||
		(from == model.StatusInReview && to == model.StatusApproved):
		var incomplete int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE ticket_id=? AND completed_at IS NULL`, id).Scan(&incomplete); err != nil {
			return err
		}
		if incomplete > 0 {
			return fmt.Errorf("ticket %s has %d incomplete task(s): complete all tasks before transitioning %s → %s", id, incomplete, from, to)
		}

	case from == model.StatusReady && to == model.StatusInProgress,
		from == model.StatusPreparing && to == model.StatusInProgress:
		var blocked int
		if err := s.db.QueryRow(`
			SELECT COUNT(*) FROM blocked_by b
			JOIN tickets bt ON bt.id = b.blocker_id
			WHERE b.ticket_id = ? AND bt.status NOT IN ('approved', 'merged')`, id).Scan(&blocked); err != nil {
			return err
		}
		if blocked > 0 {
			return fmt.Errorf("ticket %s is blocked by %d ticket(s) that are not yet approved or merged", id, blocked)
		}
	}
	return nil
}

// ReadyTicket transitions a draft ticket to ready and returns the ticket so
// the caller can set up the worktree.
func (s *Store) ReadyTicket(ticketID string) (*model.Ticket, error) {
	if err := s.TransitionTicket(ticketID, model.StatusReady); err != nil {
		return nil, err
	}
	return s.GetTicket(ticketID)
}

// SetWorktreePath updates the worktree_path, repo_path, and feature_branch on a ticket.
func (s *Store) SetWorktreePath(ticketID, worktreePath, repoPath, featureBranch string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`
		UPDATE tickets SET worktree_path=?, repo_path=?, feature_branch=?, updated=?
		WHERE id=?`,
		nullStr(worktreePath), nullStr(repoPath), featureBranch, now, ticketID)
	return err
}

// ClearWorktree clears worktree_path and feature_branch while preserving repo_path.
func (s *Store) ClearWorktree(ticketID string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`
		UPDATE tickets SET worktree_path=NULL, feature_branch='', updated=?
		WHERE id=?`,
		now, ticketID)
	return err
}

func (s *Store) DeleteTicket(id string) error {
	var status string
	if err := s.db.QueryRow(`SELECT status FROM tickets WHERE id = ?`, id).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("ticket %s not found", id)
		}
		return err
	}
	if status != string(model.StatusDraft) {
		return fmt.Errorf("cannot delete ticket %s: only draft tickets may be deleted (status: %s)", id, status)
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

	// Explicit deletion avoids relying on ON DELETE CASCADE, which requires the
	// per-connection PRAGMA foreign_keys = ON and is absent on tables created by
	// migration1 (tasks, comment_threads).
	steps := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM thread_messages WHERE thread_id IN (
			SELECT ct.id FROM comment_threads ct
			JOIN tasks t ON t.id = ct.task_id
			WHERE t.ticket_id = ?)`, []any{id}},
		{`DELETE FROM comment_threads WHERE task_id IN (
			SELECT id FROM tasks WHERE ticket_id = ?)`, []any{id}},
		{`DELETE FROM tasks WHERE ticket_id = ?`, []any{id}},
		{`DELETE FROM notes WHERE ticket_id = ?`, []any{id}},
		{`DELETE FROM blocked_by WHERE ticket_id = ? OR blocker_id = ?`, []any{id, id}},
		{`DELETE FROM tickets WHERE id = ?`, []any{id}},
	}
	for _, s := range steps {
		if _, err = tx.Exec(s.query, s.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetTicket(id string) (*model.Ticket, error) {
	row := s.db.QueryRow(`
		SELECT id, title, description, type, status, feature_branch,
		       COALESCE(worktree_path,''), COALESCE(repo_path,''), backlog, created, updated
		FROM tickets WHERE id=?`, id)
	t, err := scanTicket(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("ticket %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadBlockedBy(t); err != nil {
		return nil, err
	}
	tasks, err := s.GetTasksForTicket(id)
	if err != nil {
		return nil, err
	}
	t.Tasks = tasks
	return t, nil
}

func (s *Store) ListTickets(filter ...model.Status) ([]*model.Ticket, error) {
	var query string
	var args []interface{}
	if len(filter) == 0 {
		query = `SELECT id, title, description, type, status, feature_branch,
		               COALESCE(worktree_path,''), COALESCE(repo_path,''), backlog, created, updated
		         FROM tickets WHERE backlog=0 ORDER BY created`
	} else {
		placeholders := make([]string, len(filter))
		for i, f := range filter {
			placeholders[i] = "?"
			args = append(args, string(f))
		}
		query = fmt.Sprintf(`SELECT id, title, description, type, status, feature_branch,
		               COALESCE(worktree_path,''), COALESCE(repo_path,''), backlog, created, updated
		         FROM tickets WHERE backlog=0 AND status IN (%s) ORDER BY created`,
			strings.Join(placeholders, ","))
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var tickets []*model.Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		tickets = append(tickets, t)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}
	for _, t := range tickets {
		if err := s.loadBlockedBy(t); err != nil {
			return nil, err
		}
	}
	if err := s.loadTasksForTickets(tickets); err != nil {
		return nil, err
	}
	return tickets, nil
}

// BlockingTickets returns tickets that are blocked by the given ticket ID.
func (s *Store) BlockingTickets(blockerID string) ([]*model.Ticket, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, type, status, feature_branch,
		       COALESCE(worktree_path,''), COALESCE(repo_path,''), backlog, created, updated
		FROM tickets
		WHERE id IN (SELECT ticket_id FROM blocked_by WHERE blocker_id=?)
		ORDER BY created`, blockerID)
	if err != nil {
		return nil, err
	}
	var tickets []*model.Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		tickets = append(tickets, t)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}
	for _, t := range tickets {
		if err := s.loadBlockedBy(t); err != nil {
			return nil, err
		}
	}
	return tickets, nil
}

func (s *Store) setBlockedBy(ticketID string, blockers []string) error {
	_, err := s.db.Exec(`DELETE FROM blocked_by WHERE ticket_id=?`, ticketID)
	if err != nil {
		return err
	}
	for _, b := range blockers {
		if b == "" {
			continue
		}
		_, err = s.db.Exec(`INSERT OR IGNORE INTO blocked_by (ticket_id, blocker_id) VALUES (?, ?)`, ticketID, b)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadBlockedBy(t *model.Ticket) error {
	rows, err := s.db.Query(`SELECT blocker_id FROM blocked_by WHERE ticket_id=? ORDER BY blocker_id`, t.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return err
		}
		t.BlockedBy = append(t.BlockedBy, b)
	}
	return rows.Err()
}

func scanTicket(r scanner) (*model.Ticket, error) {
	var (
		t          model.Ticket
		typeStr    string
		statusStr  string
		backlogInt int
		createdMs  int64
		updatedMs  int64
	)
	err := r.Scan(&t.ID, &t.Title, &t.Description, &typeStr, &statusStr,
		&t.FeatureBranch, &t.WorktreePath, &t.RepoPath, &backlogInt, &createdMs, &updatedMs)
	if err != nil {
		return nil, err
	}
	t.Type = model.TicketType(typeStr)
	t.Status = model.Status(statusStr)
	t.Backlog = backlogInt != 0
	t.Created = fromMs(createdMs)
	t.Updated = fromMs(updatedMs)
	return &t, nil
}

// SetBacklog sets or clears the backlog flag on a ticket.
func (s *Store) SetBacklog(ticketID string, backlog bool) error {
	val := 0
	if backlog {
		val = 1
	}
	now := time.Now().UnixMilli()
	result, err := s.db.Exec(`UPDATE tickets SET backlog=?, updated=? WHERE id=?`, val, now, ticketID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("ticket %s not found", ticketID)
	}
	return nil
}

// ListBacklogTickets returns tickets with the backlog flag set.
func (s *Store) ListBacklogTickets() ([]*model.Ticket, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, type, status, feature_branch,
		       COALESCE(worktree_path,''), COALESCE(repo_path,''), backlog, created, updated
		FROM tickets WHERE backlog=1 ORDER BY created`)
	if err != nil {
		return nil, err
	}
	var tickets []*model.Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		tickets = append(tickets, t)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}
	for _, t := range tickets {
		if err := s.loadBlockedBy(t); err != nil {
			return nil, err
		}
	}
	if err := s.loadTasksForTickets(tickets); err != nil {
		return nil, err
	}
	return tickets, nil
}

func (s *Store) AddBlocker(ticketID, blockerID string) error {
	if ticketID == blockerID {
		return fmt.Errorf("ticket cannot block itself")
	}

	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tickets WHERE id IN (?, ?)`, ticketID, blockerID).Scan(&count)
	if err != nil {
		return err
	}
	if count < 2 {
		return fmt.Errorf("one or both tickets not found")
	}

	// Detect cycle: does blockerID already transitively depend on ticketID?
	err = s.db.QueryRow(`
		WITH RECURSIVE ancestors(id) AS (
			SELECT blocker_id FROM blocked_by WHERE ticket_id = ?
			UNION ALL
			SELECT bb.blocker_id FROM blocked_by bb JOIN ancestors a ON bb.ticket_id = a.id
		)
		SELECT COUNT(*) FROM ancestors WHERE id = ?
	`, blockerID, ticketID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("adding blocker %s to %s would create a cycle", blockerID, ticketID)
	}

	_, err = s.db.Exec(`INSERT OR IGNORE INTO blocked_by (ticket_id, blocker_id) VALUES (?, ?)`, ticketID, blockerID)
	return err
}

func (s *Store) RemoveBlocker(ticketID, blockerID string) error {
	result, err := s.db.Exec(`DELETE FROM blocked_by WHERE ticket_id=? AND blocker_id=?`, ticketID, blockerID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s is not blocked by %s", ticketID, blockerID)
	}
	return nil
}

