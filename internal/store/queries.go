package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/aidanwolter/ticket/internal/model"
)

// WorkType indicates what kind of work is available on a ticket.
type WorkType string

const (
	WorkTypeNew       WorkType = "new_work"
	WorkTypeAmendment WorkType = "amendment"
)

// WorkItem describes a ticket that has actionable work for an agent.
type WorkItem struct {
	Type   WorkType
	Ticket *model.Ticket
}

const claimableQuery = `
SELECT id, title, description, type, status, feature_branch,
       COALESCE(worktree_path,''), COALESCE(repo_path,''), created, updated
FROM tickets t
WHERE t.status = 'ready'
  AND NOT EXISTS (
    SELECT 1 FROM blocked_by b
    JOIN tickets bt ON bt.id = b.blocker_id
    WHERE b.ticket_id = t.id AND bt.status NOT IN ('approved', 'merged')
  )
ORDER BY t.created ASC`

const amendableQuery = `
SELECT DISTINCT t.id, t.title, t.description, t.type, t.status, t.feature_branch,
       COALESCE(t.worktree_path,''), COALESCE(t.repo_path,''), t.created, t.updated
FROM tickets t
JOIN tasks tk ON tk.ticket_id = t.id
JOIN comment_threads ct ON ct.task_id = tk.id
WHERE t.status = 'in_review'
  AND ct.status = 'ready'
ORDER BY t.created ASC`

// ClaimWork atomically finds and claims the first available work item using a
// BEGIN IMMEDIATE transaction so concurrent agents don't claim the same ticket.
func (s *Store) ClaimWork(author string) (*WorkItem, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Try new_work first.
	row := tx.QueryRow(claimableQuery + " LIMIT 1")
	t, scanErr := scanTicket(row)
	if scanErr != nil && scanErr != sql.ErrNoRows {
		return nil, scanErr
	}
	workType := WorkTypeNew

	// Fall back to amendment work.
	if scanErr == sql.ErrNoRows {
		row = tx.QueryRow(amendableQuery + " LIMIT 1")
		t, scanErr = scanTicket(row)
		if scanErr == sql.ErrNoRows {
			return nil, nil // no work available
		}
		if scanErr != nil {
			return nil, scanErr
		}
		workType = WorkTypeAmendment
	}

	// Validate transition.
	if err = model.ValidateTicketTransition(t.Status, model.StatusInProgress, author); err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}

	now := time.Now().UnixMilli()
	if _, err = tx.Exec(`UPDATE tickets SET status=?, updated=? WHERE id=?`,
		string(model.StatusInProgress), now, t.ID); err != nil {
		return nil, err
	}
	t.Status = model.StatusInProgress

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	// Load full ticket details outside the transaction.
	full, err := s.GetTicket(t.ID)
	if err != nil {
		return nil, err
	}
	return &WorkItem{Type: workType, Ticket: full}, nil
}

// PeekWork returns claimable work items without modifying the database.
func (s *Store) PeekWork() ([]*WorkItem, error) {
	newRows, err := s.db.Query(claimableQuery)
	if err != nil {
		return nil, err
	}
	newTickets, err := collectTickets(s, newRows)
	if err != nil {
		return nil, err
	}

	amendRows, err := s.db.Query(amendableQuery)
	if err != nil {
		return nil, err
	}
	amendTickets, err := collectTickets(s, amendRows)
	if err != nil {
		return nil, err
	}

	var items []*WorkItem
	for _, t := range newTickets {
		items = append(items, &WorkItem{Type: WorkTypeNew, Ticket: t})
	}
	for _, t := range amendTickets {
		items = append(items, &WorkItem{Type: WorkTypeAmendment, Ticket: t})
	}
	return items, nil
}

// FindWork is a deprecated alias for PeekWork kept for backwards compatibility.
func (s *Store) FindWork() ([]*WorkItem, error) {
	return s.PeekWork()
}

// AvailableWork returns ready tickets with all blockers completed.
func (s *Store) AvailableWork() ([]*model.Ticket, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, type, status, feature_branch,
		       COALESCE(worktree_path,''), COALESCE(repo_path,''), created, updated
		FROM tickets t
		WHERE t.status = 'ready'
		  AND NOT EXISTS (
		    SELECT 1 FROM blocked_by b
		    JOIN tickets bt ON bt.id = b.blocker_id
		    WHERE b.ticket_id = t.id AND bt.status NOT IN ('approved', 'merged')
		  )
		ORDER BY t.created ASC`)
	if err != nil {
		return nil, err
	}
	return collectTickets(s, rows)
}

// DraftQueue returns draft tickets.
type DraftQueue struct {
	Tickets []*model.Ticket
}

func (s *Store) DraftQueue() (*DraftQueue, error) {
	tickets, err := s.ListTickets(model.StatusDraft)
	if err != nil {
		return nil, err
	}
	return &DraftQueue{Tickets: tickets}, nil
}

// ReviewQueue returns tickets in in_review status.
type ReviewQueue struct {
	Tickets []*model.Ticket
}

func (s *Store) ReviewQueue() (*ReviewQueue, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, type, status, feature_branch,
		       COALESCE(worktree_path,''), COALESCE(repo_path,''), created, updated
		FROM tickets
		WHERE status = 'in_review'
		ORDER BY created`)
	if err != nil {
		return nil, err
	}
	tickets, err := collectTickets(s, rows)
	if err != nil {
		return nil, err
	}
	return &ReviewQueue{Tickets: tickets}, nil
}

// TicketHierarchy returns all tickets.
func (s *Store) TicketHierarchy() ([]*model.Ticket, error) {
	return s.ListTickets()
}

func collectTickets(s *Store, rows *sql.Rows) ([]*model.Ticket, error) {
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
		tasks, err := s.GetTasksForTicket(t.ID)
		if err != nil {
			return nil, err
		}
		t.Tasks = tasks
	}
	return tickets, nil
}
