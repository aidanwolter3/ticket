package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/aidanwolter/ticket/internal/ids"
	"github.com/aidanwolter/ticket/internal/model"
)

func (s *Store) CreateDraftThread(ticketID, taskID string) (*model.DraftThread, error) {
	id := ids.NewUUID()
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		`INSERT INTO draft_threads (id, ticket_id, task_id, created) VALUES (?, ?, ?, ?)`,
		id, ticketID, taskID, now)
	if err != nil {
		return nil, fmt.Errorf("create draft thread: %w", err)
	}
	return &model.DraftThread{
		ID:       id,
		TicketID: ticketID,
		TaskID:   taskID,
		Created:  time.UnixMilli(now),
	}, nil
}

func (s *Store) AddDraftMessage(threadID, ticketID string, isRealThread bool, author, text string) (*model.DraftMessage, error) {
	id := ids.NewUUID()
	now := time.Now().UnixMilli()
	isReal := 0
	if isRealThread {
		isReal = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO draft_messages (id, thread_id, ticket_id, is_real_thread, author, text, created) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, threadID, ticketID, isReal, author, text, now)
	if err != nil {
		return nil, fmt.Errorf("add draft message: %w", err)
	}
	return &model.DraftMessage{
		ID:           id,
		ThreadID:     threadID,
		TicketID:     ticketID,
		IsRealThread: isRealThread,
		Author:       author,
		Text:         text,
		Created:      time.UnixMilli(now),
	}, nil
}

func (s *Store) SetDraftAction(threadID, ticketID, action string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO draft_actions (thread_id, ticket_id, action, created) VALUES (?, ?, ?, ?)`,
		threadID, ticketID, action, now)
	return err
}

func (s *Store) ClearDraftAction(threadID string) error {
	_, err := s.db.Exec(`DELETE FROM draft_actions WHERE thread_id=?`, threadID)
	return err
}

func (s *Store) DeleteDraftMessage(id string) error {
	_, err := s.db.Exec(`DELETE FROM draft_messages WHERE id=?`, id)
	return err
}

func (s *Store) UpdateDraftMessage(id, text string) error {
	_, err := s.db.Exec(`UPDATE draft_messages SET text=? WHERE id=?`, text, id)
	return err
}

func (s *Store) GetDraftThread(id string) (*model.DraftThread, error) {
	row := s.db.QueryRow(`SELECT id, ticket_id, task_id, created FROM draft_threads WHERE id=?`, id)
	dt, err := scanDraftThread(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("draft thread %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	msgs, err := s.loadDraftMessagesForThread(dt.ID, false)
	if err != nil {
		return nil, err
	}
	dt.Messages = msgs
	return dt, nil
}

func (s *Store) GetDraftState(ticketID string) (*model.DraftState, error) {
	state := &model.DraftState{TicketID: ticketID}

	rows, err := s.db.Query(
		`SELECT id, ticket_id, task_id, created FROM draft_threads WHERE ticket_id=? ORDER BY created`,
		ticketID)
	if err != nil {
		return nil, err
	}
	var threads []model.DraftThread
	for rows.Next() {
		dt, err := scanDraftThread(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		threads = append(threads, *dt)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for i := range threads {
		msgs, err := s.loadDraftMessagesForThread(threads[i].ID, false)
		if err != nil {
			return nil, err
		}
		threads[i].Messages = msgs
	}
	state.NewThreads = threads

	replyRows, err := s.db.Query(
		`SELECT id, thread_id, ticket_id, is_real_thread, author, text, created FROM draft_messages WHERE ticket_id=? AND is_real_thread=1 ORDER BY created`,
		ticketID)
	if err != nil {
		return nil, err
	}
	for replyRows.Next() {
		m, err := scanDraftMessage(replyRows)
		if err != nil {
			replyRows.Close()
			return nil, err
		}
		state.Replies = append(state.Replies, *m)
	}
	if err := replyRows.Err(); err != nil {
		replyRows.Close()
		return nil, err
	}
	replyRows.Close()

	actionRows, err := s.db.Query(
		`SELECT thread_id, ticket_id, action, created FROM draft_actions WHERE ticket_id=? ORDER BY created`,
		ticketID)
	if err != nil {
		return nil, err
	}
	for actionRows.Next() {
		var da model.DraftAction
		var createdMs int64
		if err := actionRows.Scan(&da.ThreadID, &da.TicketID, &da.Action, &createdMs); err != nil {
			actionRows.Close()
			return nil, err
		}
		da.Created = time.UnixMilli(createdMs)
		state.Actions = append(state.Actions, da)
	}
	if err := actionRows.Err(); err != nil {
		actionRows.Close()
		return nil, err
	}
	actionRows.Close()

	return state, nil
}

// FlushDraftState atomically applies all staged draft actions for a ticket to the
// real store and clears the draft state. It does NOT transition the ticket status.
func (s *Store) FlushDraftState(ticketID string) error {
	state, err := s.GetDraftState(ticketID)
	if err != nil {
		return fmt.Errorf("flush draft: get state: %w", err)
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

	// 1. Commit new draft threads as real threads (status: needs_attention).
	for _, dt := range state.NewThreads {
		threadID := ids.NewUUID()
		now := time.Now().UnixMilli()
		if _, err = tx.Exec(
			`INSERT INTO comment_threads (id, task_id, status, created) VALUES (?, ?, 'needs_attention', ?)`,
			threadID, dt.TaskID, now); err != nil {
			return fmt.Errorf("flush: create thread: %w", err)
		}
		for _, msg := range dt.Messages {
			msgID := ids.NewUUID()
			msgNow := time.Now().UnixMilli()
			if _, err = tx.Exec(
				`INSERT INTO thread_messages (id, thread_id, author, text, created) VALUES (?, ?, ?, ?, ?)`,
				msgID, threadID, msg.Author, msg.Text, msgNow); err != nil {
				return fmt.Errorf("flush: add thread message: %w", err)
			}
		}
		if _, err = tx.Exec(`DELETE FROM draft_messages WHERE thread_id=? AND is_real_thread=0`, dt.ID); err != nil {
			return fmt.Errorf("flush: delete draft messages: %w", err)
		}
		if _, err = tx.Exec(`DELETE FROM draft_threads WHERE id=?`, dt.ID); err != nil {
			return fmt.Errorf("flush: delete draft thread: %w", err)
		}
	}

	// Build action map for quick lookup.
	actionMap := make(map[string]string)
	for _, a := range state.Actions {
		actionMap[a.ThreadID] = a.Action
	}

	// 2. Commit draft replies; track which real threads received replies.
	replyThreads := make(map[string]bool)
	for _, reply := range state.Replies {
		msgID := ids.NewUUID()
		msgNow := time.Now().UnixMilli()
		if _, err = tx.Exec(
			`INSERT INTO thread_messages (id, thread_id, author, text, created) VALUES (?, ?, ?, ?, ?)`,
			msgID, reply.ThreadID, reply.Author, reply.Text, msgNow); err != nil {
			return fmt.Errorf("flush: add reply: %w", err)
		}
		replyThreads[reply.ThreadID] = true
		if _, err = tx.Exec(`DELETE FROM draft_messages WHERE id=?`, reply.ID); err != nil {
			return fmt.Errorf("flush: delete draft reply: %w", err)
		}
	}

	// 3. Update status for threads that received replies (staged action overrides reply).
	for threadID := range replyThreads {
		targetStatus := string(model.ThreadNeedsAttention)
		if act, ok := actionMap[threadID]; ok {
			switch act {
			case model.DraftActionResolve:
				targetStatus = string(model.ThreadResolved)
			case model.DraftActionReopen:
				targetStatus = string(model.ThreadOpen)
			}
			delete(actionMap, threadID) // processed here, skip below
		}
		if _, err = tx.Exec(`UPDATE comment_threads SET status=? WHERE id=?`, targetStatus, threadID); err != nil {
			return fmt.Errorf("flush: set reply thread status: %w", err)
		}
	}

	// 4. Apply remaining staged actions (threads that did not receive a reply).
	for threadID, action := range actionMap {
		var targetStatus string
		switch action {
		case model.DraftActionResolve:
			targetStatus = string(model.ThreadResolved)
		default:
			targetStatus = string(model.ThreadOpen)
		}
		if _, err = tx.Exec(`UPDATE comment_threads SET status=? WHERE id=?`, targetStatus, threadID); err != nil {
			return fmt.Errorf("flush: set action thread status: %w", err)
		}
	}

	// 5. Delete all staged actions for this ticket.
	if _, err = tx.Exec(`DELETE FROM draft_actions WHERE ticket_id=?`, ticketID); err != nil {
		return fmt.Errorf("flush: delete draft actions: %w", err)
	}

	return tx.Commit()
}

func (s *Store) loadDraftMessagesForThread(threadID string, isRealThread bool) ([]model.DraftMessage, error) {
	isReal := 0
	if isRealThread {
		isReal = 1
	}
	rows, err := s.db.Query(
		`SELECT id, thread_id, ticket_id, is_real_thread, author, text, created FROM draft_messages WHERE thread_id=? AND is_real_thread=? ORDER BY created`,
		threadID, isReal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []model.DraftMessage
	for rows.Next() {
		m, err := scanDraftMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, *m)
	}
	return msgs, rows.Err()
}

func scanDraftThread(r scanner) (*model.DraftThread, error) {
	var (
		dt        model.DraftThread
		createdMs int64
	)
	if err := r.Scan(&dt.ID, &dt.TicketID, &dt.TaskID, &createdMs); err != nil {
		return nil, err
	}
	dt.Created = time.UnixMilli(createdMs)
	return &dt, nil
}

func scanDraftMessage(r scanner) (*model.DraftMessage, error) {
	var (
		m         model.DraftMessage
		isReal    int
		createdMs int64
	)
	if err := r.Scan(&m.ID, &m.ThreadID, &m.TicketID, &isReal, &m.Author, &m.Text, &createdMs); err != nil {
		return nil, err
	}
	m.IsRealThread = isReal == 1
	m.Created = time.UnixMilli(createdMs)
	return &m, nil
}
