package store

import (
	"database/sql"

	"github.com/aidanwolter/ticket/internal/model"
)

// AvailableWork returns ready tickets with all blockers completed.
func (s *Store) AvailableWork() ([]*model.Ticket, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, type, status, feature_branch,
		       COALESCE(worktree_path,''), COALESCE(repo_path,''), backlog, created, updated
		FROM tickets t
		WHERE t.status = 'ready'
		  AND t.backlog = 0
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
		       COALESCE(worktree_path,''), COALESCE(repo_path,''), backlog, created, updated
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
