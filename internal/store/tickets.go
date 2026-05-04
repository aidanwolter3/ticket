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
		INSERT INTO tickets (id, title, description, type, status, feature_branch, stack_id, commit_hash, verifiable_result, created, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Title, t.Description, string(t.Type), string(t.Status),
		t.FeatureBranch, nullStr(t.StackID), nullStr(t.CommitHash),
		t.VerifiableResult, now, now,
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
		  stack_id=?, commit_hash=?, verifiable_result=?, updated=?
		WHERE id=?`,
		t.Title, t.Description, string(t.Type), string(t.Status),
		t.FeatureBranch, nullStr(t.StackID), nullStr(t.CommitHash),
		t.VerifiableResult, now, t.ID,
	)
	if err != nil {
		return fmt.Errorf("update ticket: %w", err)
	}
	return s.setBlockedBy(t.ID, t.BlockedBy)
}

func (s *Store) TransitionTicket(id string, to model.Status, author string) error {
	var fromStr, typeStr string
	err := s.db.QueryRow(`SELECT status, type FROM tickets WHERE id=?`, id).Scan(&fromStr, &typeStr)
	if err == sql.ErrNoRows {
		return fmt.Errorf("ticket %s not found", id)
	}
	if err != nil {
		return err
	}
	if model.TicketType(typeStr) == model.TypePlan {
		return fmt.Errorf("plan status is auto-derived from child tickets and cannot be set directly")
	}
	if err := model.ValidateTicketTransition(model.Status(fromStr), to, author); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	if _, err = s.db.Exec(`UPDATE tickets SET status=?, updated=? WHERE id=?`, string(to), now, id); err != nil {
		return err
	}
	return s.updateParentPlanStatus(id)
}

// updateParentPlanStatus recalculates and persists the status for any plan that has id as a child.
func (s *Store) updateParentPlanStatus(childID string) error {
	rows, err := s.db.Query(`
		SELECT t.id FROM tickets t
		JOIN blocked_by b ON b.ticket_id = t.id
		WHERE b.blocker_id = ? AND t.type = 'plan'`, childID)
	if err != nil {
		return err
	}
	var planIDs []string
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err != nil {
			rows.Close()
			return err
		}
		planIDs = append(planIDs, pid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, planID := range planIDs {
		if err := s.recalcPlanStatus(planID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) recalcPlanStatus(planID string) error {
	rows, err := s.db.Query(`
		SELECT t.status FROM tickets t
		JOIN blocked_by b ON b.ticket_id = ? AND b.blocker_id = t.id`, planID)
	if err != nil {
		return err
	}
	var statuses []model.Status
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			rows.Close()
			return err
		}
		statuses = append(statuses, model.Status(st))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	derived := model.DerivePlanStatus(statuses)
	now := time.Now().UnixMilli()
	_, err = s.db.Exec(`UPDATE tickets SET status=?, updated=? WHERE id=?`, string(derived), now, planID)
	return err
}

// PromoteDraftChildren transitions all draft direct children of a plan to ready.
func (s *Store) PromoteDraftChildren(planID, author string) ([]*model.Ticket, error) {
	plan, err := s.GetTicket(planID)
	if err != nil {
		return nil, err
	}
	if !plan.IsPlan() {
		return nil, fmt.Errorf("%s is not a plan", planID)
	}
	var promoted []*model.Ticket
	for _, childID := range plan.BlockedBy {
		child, err := s.GetTicket(childID)
		if err != nil {
			continue
		}
		if child.Status != model.StatusDraft {
			continue
		}
		if err := s.TransitionTicket(childID, model.StatusReady, author); err != nil {
			return nil, fmt.Errorf("failed to promote %s: %w", childID, err)
		}
		child.Status = model.StatusReady
		promoted = append(promoted, child)
	}
	return promoted, nil
}

func (s *Store) DeleteTicket(id string) error {
	_, err := s.db.Exec(`DELETE FROM tickets WHERE id=?`, id)
	return err
}

func (s *Store) GetTicket(id string) (*model.Ticket, error) {
	row := s.db.QueryRow(`SELECT id, title, description, type, status, feature_branch, COALESCE(stack_id,''), COALESCE(commit_hash,''), verifiable_result, created, updated FROM tickets WHERE id=?`, id)
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
	return t, nil
}

func (s *Store) ListTickets(filter ...model.Status) ([]*model.Ticket, error) {
	var query string
	var args []interface{}
	if len(filter) == 0 {
		query = `SELECT id, title, description, type, status, feature_branch, COALESCE(stack_id,''), COALESCE(commit_hash,''), verifiable_result, created, updated FROM tickets ORDER BY created`
	} else {
		placeholders := make([]string, len(filter))
		for i, f := range filter {
			placeholders[i] = "?"
			args = append(args, string(f))
		}
		query = fmt.Sprintf(`SELECT id, title, description, type, status, feature_branch, COALESCE(stack_id,''), COALESCE(commit_hash,''), verifiable_result, created, updated FROM tickets WHERE status IN (%s) ORDER BY created`, strings.Join(placeholders, ","))
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
	rows.Close() // close before loadBlockedBy to release the single connection
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
		&t.FeatureBranch, &t.StackID, &t.CommitHash,
		&t.VerifiableResult, &createdMs, &updatedMs)
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
