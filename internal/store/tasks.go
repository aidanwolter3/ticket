package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aidanwolter/ticket/internal/ids"
	"github.com/aidanwolter/ticket/internal/model"
)

func (s *Store) nextTaskID() (string, error) {
	var max sql.NullString
	err := s.db.QueryRow(`SELECT MAX(CAST(SUBSTR(id, 4) AS INTEGER)) FROM tasks`).Scan(&max)
	if err != nil {
		return "", err
	}
	n := 1
	if max.Valid {
		fmt.Sscanf(max.String, "%d", &n)
		n++
	}
	return ids.TaskID(n), nil
}

func (s *Store) CreateTask(t *model.Task) error {
	id, err := s.nextTaskID()
	if err != nil {
		return fmt.Errorf("generate task id: %w", err)
	}
	t.ID = id
	now := time.Now().UnixMilli()
	t.Created = time.UnixMilli(now)
	t.Updated = time.UnixMilli(now)
	if t.Round == 0 {
		t.Round = 1
	}

	_, err = s.db.Exec(`
		INSERT INTO tasks (id, ticket_id, title, description, position, round,
		  commit_hash, verifiable_result, completed_at, created, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.TicketID, t.Title, t.Description, t.Position, t.Round,
		nullStr(t.CommitHash), t.VerifiableResult, nullTime(t.CompletedAt),
		now, now,
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

func (s *Store) UpdateTask(t *model.Task) error {
	now := time.Now().UnixMilli()
	t.Updated = time.UnixMilli(now)
	_, err := s.db.Exec(`
		UPDATE tasks SET title=?, description=?, position=?, round=?,
		  commit_hash=?, verifiable_result=?, completed_at=?, updated=?
		WHERE id=?`,
		t.Title, t.Description, t.Position, t.Round,
		nullStr(t.CommitHash), t.VerifiableResult, nullTime(t.CompletedAt),
		now, t.ID,
	)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	return nil
}

func (s *Store) GetTask(id string) (*model.Task, error) {
	row := s.db.QueryRow(`
		SELECT id, ticket_id, title, description, position, round,
		       COALESCE(commit_hash,''), verifiable_result, completed_at, created, updated
		FROM tasks WHERE id=?`, id)
	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task %s not found", id)
	}
	return t, err
}

func (s *Store) GetTasksForTicket(ticketID string) ([]model.Task, error) {
	rows, err := s.db.Query(`
		SELECT id, ticket_id, title, description, position, round,
		       COALESCE(commit_hash,''), verifiable_result, completed_at, created, updated
		FROM tasks WHERE ticket_id=? ORDER BY position`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []model.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

// loadTasksForTickets populates Tasks on each ticket using a single batch query.
func (s *Store) loadTasksForTickets(tickets []*model.Ticket) error {
	if len(tickets) == 0 {
		return nil
	}
	idx := make(map[string]*model.Ticket, len(tickets))
	placeholders := make([]string, len(tickets))
	args := make([]interface{}, len(tickets))
	for i, t := range tickets {
		idx[t.ID] = t
		placeholders[i] = "?"
		args[i] = t.ID
	}
	query := fmt.Sprintf(`
		SELECT id, ticket_id, title, description, position, round,
		       COALESCE(commit_hash,''), verifiable_result, completed_at, created, updated
		FROM tasks WHERE ticket_id IN (%s) ORDER BY ticket_id, position`,
		strings.Join(placeholders, ","))
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return err
		}
		ticket := idx[t.TicketID]
		ticket.Tasks = append(ticket.Tasks, *t)
	}
	return rows.Err()
}

// CompleteTask sets completed_at to now. Called by the TUI threads view ("c" keybinding on a task row).
func (s *Store) CompleteTask(id string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`UPDATE tasks SET completed_at=?, updated=? WHERE id=?`, now, now, id)
	return err
}

// UncompleteTask clears completed_at. Called by the TUI threads view ("c" keybinding on a task row).
func (s *Store) UncompleteTask(id string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`UPDATE tasks SET completed_at=NULL, updated=? WHERE id=?`, now, id)
	return err
}

// DeleteTask removes a task by ID. Returns an error if the task does not exist
// or has already been completed.
func (s *Store) DeleteTask(id string) error {
	t, err := s.GetTask(id)
	if err != nil {
		return err
	}
	if t.CompletedAt != nil {
		return fmt.Errorf("task %s is already completed and cannot be deleted", id)
	}
	_, err = s.db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	return err
}

// LastTaskForTicket returns the task with the highest position for a ticket.
// Used by the TUI to choose which task to attach new threads to.
func (s *Store) LastTaskForTicket(ticketID string) (*model.Task, error) {
	row := s.db.QueryRow(`
		SELECT id, ticket_id, title, description, position, round,
		       COALESCE(commit_hash,''), verifiable_result, completed_at, created, updated
		FROM tasks WHERE ticket_id=? ORDER BY position DESC LIMIT 1`, ticketID)
	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func scanTask(r scanner) (*model.Task, error) {
	var (
		t           model.Task
		completedMs sql.NullInt64
		createdMs   int64
		updatedMs   int64
	)
	err := r.Scan(&t.ID, &t.TicketID, &t.Title, &t.Description, &t.Position, &t.Round,
		&t.CommitHash, &t.VerifiableResult, &completedMs, &createdMs, &updatedMs)
	if err != nil {
		return nil, err
	}
	if t.Round == 0 {
		t.Round = 1
	}
	if completedMs.Valid {
		ts := time.UnixMilli(completedMs.Int64)
		t.CompletedAt = &ts
	}
	t.Created = time.UnixMilli(createdMs)
	t.Updated = time.UnixMilli(updatedMs)
	return &t, nil
}

func nullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}
