package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/aidanwolter/ticket/internal/ids"
	"github.com/aidanwolter/ticket/internal/model"
)

func (s *Store) GetThread(id string) (*model.Thread, error) {
	row := s.db.QueryRow(`SELECT id, task_id, status, created FROM comment_threads WHERE id=?`, id)
	t, err := scanThread(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("thread %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadMessages(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) CreateThread(taskID string) (*model.Thread, error) {
	id := ids.NewUUID()
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO comment_threads (id, task_id, status, created) VALUES (?, ?, 'open', ?)`,
		id, taskID, now)
	if err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}
	return &model.Thread{
		ID:      id,
		TaskID:  taskID,
		Status:  model.ThreadOpen,
		Created: time.UnixMilli(now),
	}, nil
}

func (s *Store) TransitionThread(id string, to model.ThreadStatus, author string) error {
	var fromStr string
	err := s.db.QueryRow(`SELECT status FROM comment_threads WHERE id=?`, id).Scan(&fromStr)
	if err == sql.ErrNoRows {
		return fmt.Errorf("thread %s not found", id)
	}
	if err != nil {
		return err
	}
	if err := model.ValidateThreadTransition(model.ThreadStatus(fromStr), to, author); err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE comment_threads SET status=? WHERE id=?`, string(to), id)
	return err
}

func (s *Store) AddMessage(threadID, author, text string) (*model.Message, error) {
	id := ids.NewUUID()
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO thread_messages (id, thread_id, author, text, created) VALUES (?, ?, ?, ?, ?)`,
		id, threadID, author, text, now)
	if err != nil {
		return nil, fmt.Errorf("add message: %w", err)
	}
	return &model.Message{
		ID:       id,
		ThreadID: threadID,
		Author:   author,
		Text:     text,
		Created:  time.UnixMilli(now),
	}, nil
}

func (s *Store) GetThreadsForTask(taskID string) ([]*model.Thread, error) {
	rows, err := s.db.Query(`SELECT id, task_id, status, created FROM comment_threads WHERE task_id=? ORDER BY created`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var threads []*model.Thread
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, t := range threads {
		if err := s.loadMessages(t); err != nil {
			return nil, err
		}
	}
	return threads, nil
}

// GetThreadsForTicket returns all threads across all tasks of a ticket.
func (s *Store) GetThreadsForTicket(ticketID string) ([]*model.Thread, error) {
	return s.GetAllThreadsForTicket(ticketID)
}

// GetAllThreadsForTicket returns all threads across all tasks of a ticket,
// ordered by task position then thread creation time.
func (s *Store) GetAllThreadsForTicket(ticketID string) ([]*model.Thread, error) {
	rows, err := s.db.Query(`
		SELECT ct.id, ct.task_id, ct.status, ct.created
		FROM comment_threads ct
		JOIN tasks tk ON tk.id = ct.task_id
		WHERE tk.ticket_id = ?
		ORDER BY tk.position, ct.created`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var threads []*model.Thread
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, t := range threads {
		if err := s.loadMessages(t); err != nil {
			return nil, err
		}
	}
	return threads, nil
}

func (s *Store) loadMessages(t *model.Thread) error {
	rows, err := s.db.Query(`SELECT id, thread_id, author, text, created FROM thread_messages WHERE thread_id=? ORDER BY created`, t.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			m         model.Message
			createdMs int64
		)
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Author, &m.Text, &createdMs); err != nil {
			return err
		}
		m.Created = time.UnixMilli(createdMs)
		t.Messages = append(t.Messages, m)
	}
	return rows.Err()
}

func scanThread(r scanner) (*model.Thread, error) {
	var (
		t         model.Thread
		statusStr string
		createdMs int64
	)
	if err := r.Scan(&t.ID, &t.TaskID, &statusStr, &createdMs); err != nil {
		return nil, err
	}
	t.Status = model.ThreadStatus(statusStr)
	t.Created = time.UnixMilli(createdMs)
	return &t, nil
}
