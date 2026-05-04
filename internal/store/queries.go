package store

import (
	"database/sql"

	"github.com/aidanwolter/ticket/internal/model"
)

// AvailableWork returns ready tickets (not plans) whose blockers are all completed.
func (s *Store) AvailableWork() ([]*model.Ticket, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, type, status, feature_branch,
		       COALESCE(stack_id,''), COALESCE(commit_hash,''), verifiable_result, created, updated
		FROM tickets t
		WHERE t.status = 'ready'
		  AND t.type = 'ticket'
		  AND NOT EXISTS (
		    SELECT 1 FROM blocked_by b
		    JOIN tickets bt ON bt.id = b.blocker_id
		    WHERE b.ticket_id = t.id AND bt.status != 'completed'
		  )
		ORDER BY t.created ASC`)
	if err != nil {
		return nil, err
	}
	return collectTickets(s, rows)
}

// ReviewQueue returns stacks (all tickets in_review) and standalone in_review tickets.
type ReviewQueue struct {
	Stacks     map[string][]*model.Ticket // stack_id → tickets
	Standalone []*model.Ticket
}

func (s *Store) ReviewQueue() (*ReviewQueue, error) {
	// Stacks where all tickets are in_review
	stackRows, err := s.db.Query(`
		WITH stack_status AS (
		  SELECT stack_id,
		         COUNT(*) AS total,
		         SUM(CASE WHEN status = 'in_review' THEN 1 ELSE 0 END) AS in_review_count
		  FROM tickets
		  WHERE stack_id IS NOT NULL
		  GROUP BY stack_id
		)
		SELECT t.id, t.title, t.description, t.type, t.status, t.feature_branch,
		       COALESCE(t.stack_id,''), COALESCE(t.commit_hash,''), t.verifiable_result, t.created, t.updated
		FROM tickets t
		JOIN stack_status s ON s.stack_id = t.stack_id
		WHERE s.total = s.in_review_count
		ORDER BY t.stack_id, t.created`)
	if err != nil {
		return nil, err
	}
	stackTickets, err := collectTickets(s, stackRows)
	if err != nil {
		return nil, err
	}

	stacks := make(map[string][]*model.Ticket)
	for _, t := range stackTickets {
		stacks[t.StackID] = append(stacks[t.StackID], t)
	}

	// Standalone in_review tickets
	soloRows, err := s.db.Query(`
		SELECT id, title, description, type, status, feature_branch,
		       COALESCE(stack_id,''), COALESCE(commit_hash,''), verifiable_result, created, updated
		FROM tickets
		WHERE status = 'in_review' AND stack_id IS NULL
		ORDER BY created`)
	if err != nil {
		return nil, err
	}
	standalone, err := collectTickets(s, soloRows)
	if err != nil {
		return nil, err
	}

	return &ReviewQueue{Stacks: stacks, Standalone: standalone}, nil
}

// TicketHierarchy returns plans first with BlockedBy populated, then standalone tickets.
func (s *Store) TicketHierarchy() ([]*model.Ticket, error) {
	return s.ListTickets()
}

// BlockingTickets returns tickets that have id in their blocked_by list.
func (s *Store) BlockingTickets(id string) ([]*model.Ticket, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.title, t.description, t.type, t.status, t.feature_branch,
		       COALESCE(t.stack_id,''), COALESCE(t.commit_hash,''), t.verifiable_result, t.created, t.updated
		FROM tickets t
		JOIN blocked_by b ON b.ticket_id = t.id
		WHERE b.blocker_id = ?
		ORDER BY t.created`, id)
	if err != nil {
		return nil, err
	}
	return collectTickets(s, rows)
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
	rows.Close() // close before loadBlockedBy to free the single connection
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
