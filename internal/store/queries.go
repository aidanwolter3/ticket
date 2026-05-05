package store

import (
	"database/sql"

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

// FindWork returns tickets with actionable work for agents:
//   - new_work:  ready tickets with all blockers completed and no worktree claimed
//   - amendment: in_review tickets that have at least one ready thread across their tasks
func (s *Store) FindWork() ([]*WorkItem, error) {
	// New work: ready tickets, all blockers completed, no worktree claimed.
	newRows, err := s.db.Query(`
		SELECT id, title, description, type, status, feature_branch,
		       COALESCE(worktree_path,''), created, updated
		FROM tickets t
		WHERE t.status = 'ready'
		  AND t.worktree_path IS NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM blocked_by b
		    JOIN tickets bt ON bt.id = b.blocker_id
		    WHERE b.ticket_id = t.id AND bt.status != 'completed'
		  )
		ORDER BY t.created ASC`)
	if err != nil {
		return nil, err
	}
	newTickets, err := collectTickets(s, newRows)
	if err != nil {
		return nil, err
	}

	// Amendment work: in_review tickets that have ready threads on any task.
	amendRows, err := s.db.Query(`
		SELECT DISTINCT t.id, t.title, t.description, t.type, t.status, t.feature_branch,
		       COALESCE(t.worktree_path,''), t.created, t.updated
		FROM tickets t
		JOIN tasks tk ON tk.ticket_id = t.id
		JOIN comment_threads ct ON ct.task_id = tk.id
		WHERE t.status = 'in_review'
		  AND ct.status = 'ready'
		ORDER BY t.created ASC`)
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
		       COALESCE(worktree_path,''), created, updated
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
