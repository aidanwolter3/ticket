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
		  no_commit, commit_hash, verifiable_result, completed_at, created, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.TicketID, t.Title, t.Description, t.Position, t.Round,
		t.NoCommit, nullStr(t.CommitHash), t.VerifiableResult, nullTime(t.CompletedAt),
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
		  no_commit=?, commit_hash=?, verifiable_result=?, completed_at=?, updated=?
		WHERE id=?`,
		t.Title, t.Description, t.Position, t.Round,
		t.NoCommit, nullStr(t.CommitHash), t.VerifiableResult, nullTime(t.CompletedAt),
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
		       no_commit, COALESCE(commit_hash,''), verifiable_result, completed_at, created, updated
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
		       no_commit, COALESCE(commit_hash,''), verifiable_result, completed_at, created, updated
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
		       no_commit, COALESCE(commit_hash,''), verifiable_result, completed_at, created, updated
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

// CompleteTaskWithCommit sets completed_at to now and records the commit hash.
func (s *Store) CompleteTaskWithCommit(id, commitHash string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`UPDATE tasks SET completed_at=?, commit_hash=?, updated=? WHERE id=?`, now, commitHash, now, id)
	return err
}

// UncompleteTask clears completed_at. Called by the TUI threads view ("c" keybinding on a task row).
func (s *Store) UncompleteTask(id string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`UPDATE tasks SET completed_at=NULL, updated=? WHERE id=?`, now, id)
	return err
}

// ResetTasksForTicket clears completed_at on all tasks for the given ticket,
// returning them to pending status.
func (s *Store) ResetTasksForTicket(ticketID string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`UPDATE tasks SET completed_at=NULL, updated=? WHERE ticket_id=?`, now, ticketID)
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

// MoveTask changes a task's position within its ticket, shifting other tasks to accommodate.
func (s *Store) MoveTask(taskID string, newPos int) error {
	task, err := s.GetTask(taskID)
	if err != nil {
		return err
	}

	tasks, err := s.GetTasksForTicket(task.TicketID)
	if err != nil {
		return err
	}

	if newPos < 1 || newPos > len(tasks) {
		return fmt.Errorf("position %d out of range [1, %d]", newPos, len(tasks))
	}

	oldPos := task.Position
	if oldPos == newPos {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UnixMilli()

	if newPos < oldPos {
		_, err = tx.Exec(
			`UPDATE tasks SET position=position+1, updated=? WHERE ticket_id=? AND position>=? AND position<?`,
			now, task.TicketID, newPos, oldPos,
		)
	} else {
		_, err = tx.Exec(
			`UPDATE tasks SET position=position-1, updated=? WHERE ticket_id=? AND position>? AND position<=?`,
			now, task.TicketID, oldPos, newPos,
		)
	}
	if err != nil {
		return fmt.Errorf("shift tasks: %w", err)
	}

	_, err = tx.Exec(`UPDATE tasks SET position=?, updated=? WHERE id=?`, newPos, now, taskID)
	if err != nil {
		return fmt.Errorf("set task position: %w", err)
	}

	return tx.Commit()
}

// LastTaskForTicket returns the task with the highest position for a ticket.
// Used by the TUI to choose which task to attach new threads to.
func (s *Store) LastTaskForTicket(ticketID string) (*model.Task, error) {
	row := s.db.QueryRow(`
		SELECT id, ticket_id, title, description, position, round,
		       no_commit, COALESCE(commit_hash,''), verifiable_result, completed_at, created, updated
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
		&t.NoCommit, &t.CommitHash, &t.VerifiableResult, &completedMs, &createdMs, &updatedMs)
	if err != nil {
		return nil, err
	}
	if t.Round == 0 {
		t.Round = 1
	}
	t.CompletedAt = fromNullMs(completedMs)
	t.Created = fromMs(createdMs)
	t.Updated = fromMs(updatedMs)
	return &t, nil
}

