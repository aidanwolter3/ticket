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
	id, err := s.nextTicketID()
	if err != nil {
		return fmt.Errorf("generate id: %w", err)
	}
	t.ID = id
	now := time.Now().UnixMilli()
	t.Created = time.UnixMilli(now)
	t.Updated = time.UnixMilli(now)

	_, err = s.db.Exec(`
		INSERT INTO tickets (id, title, description, type, status, feature_branch, worktree_path, created, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Title, t.Description, string(t.Type), string(t.Status),
		t.FeatureBranch, nullStr(t.WorktreePath), now, now,
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
		  worktree_path=?, updated=?
		WHERE id=?`,
		t.Title, t.Description, string(t.Type), string(t.Status),
		t.FeatureBranch, nullStr(t.WorktreePath), now, t.ID,
	)
	if err != nil {
		return fmt.Errorf("update ticket: %w", err)
	}
	return s.setBlockedBy(t.ID, t.BlockedBy)
}

func (s *Store) TransitionTicket(id string, to model.Status, author string) error {
	var fromStr string
	err := s.db.QueryRow(`SELECT status FROM tickets WHERE id=?`, id).Scan(&fromStr)
	if err == sql.ErrNoRows {
		return fmt.Errorf("ticket %s not found", id)
	}
	if err != nil {
		return err
	}
	if err := model.ValidateTicketTransition(model.Status(fromStr), to, author); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	_, err = s.db.Exec(`UPDATE tickets SET status=?, updated=? WHERE id=?`, string(to), now, id)
	return err
}

// PromoteTicket transitions a draft ticket to ready.
func (s *Store) PromoteTicket(ticketID, author string) error {
	return s.TransitionTicket(ticketID, model.StatusReady, author)
}

func (s *Store) DeleteTicket(id string) error {
	_, err := s.db.Exec(`DELETE FROM tickets WHERE id=?`, id)
	return err
}

func (s *Store) GetTicket(id string) (*model.Ticket, error) {
	row := s.db.QueryRow(`
		SELECT id, title, description, type, status, feature_branch,
		       COALESCE(worktree_path,''), created, updated
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
		               COALESCE(worktree_path,''), created, updated
		         FROM tickets ORDER BY created`
	} else {
		placeholders := make([]string, len(filter))
		for i, f := range filter {
			placeholders[i] = "?"
			args = append(args, string(f))
		}
		query = fmt.Sprintf(`SELECT id, title, description, type, status, feature_branch,
		               COALESCE(worktree_path,''), created, updated
		         FROM tickets WHERE status IN (%s) ORDER BY created`,
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

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanTicket(r scanner) (*model.Ticket, error) {
	var (
		t         model.Ticket
		typeStr   string
		statusStr string
		createdMs int64
		updatedMs int64
	)
	err := r.Scan(&t.ID, &t.Title, &t.Description, &typeStr, &statusStr,
		&t.FeatureBranch, &t.WorktreePath, &createdMs, &updatedMs)
	if err != nil {
		return nil, err
	}
	t.Type = model.TicketType(typeStr)
	t.Status = model.Status(statusStr)
	t.Created = time.UnixMilli(createdMs)
	t.Updated = time.UnixMilli(updatedMs)
	return &t, nil
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
