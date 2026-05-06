package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/aidanwolter/ticket/internal/ids"
	"github.com/aidanwolter/ticket/internal/model"
)

func (s *Store) CreateAgentSession(ticketID string, pid int, logPath string) (*model.AgentSession, error) {
	id := ids.NewUUID()
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`
		INSERT INTO agent_sessions (id, ticket_id, pid, started_at, state, log_path)
		VALUES (?, ?, ?, ?, 'running', ?)`,
		id, ticketID, pid, now, logPath,
	)
	if err != nil {
		return nil, fmt.Errorf("create agent session: %w", err)
	}
	return &model.AgentSession{
		ID:        id,
		TicketID:  ticketID,
		PID:       pid,
		StartedAt: time.UnixMilli(now),
		State:     model.AgentRunning,
		LogPath:   logPath,
	}, nil
}

func (s *Store) GetAgentSessionByTicket(ticketID string) (*model.AgentSession, error) {
	row := s.db.QueryRow(`
		SELECT id, ticket_id, pid, started_at, state, log_path
		FROM agent_sessions
		WHERE ticket_id = ? AND state IN ('running', 'waiting')
		ORDER BY started_at DESC
		LIMIT 1`, ticketID)
	return scanAgentSession(row)
}

func (s *Store) UpdateAgentSessionState(id string, state model.AgentState) error {
	_, err := s.db.Exec(`UPDATE agent_sessions SET state = ? WHERE id = ?`, string(state), id)
	return err
}

// TerminateAllAgentSessions marks all running/waiting sessions as terminated.
func (s *Store) TerminateAllAgentSessions() error {
	_, err := s.db.Exec(`
		UPDATE agent_sessions SET state = 'terminated'
		WHERE state IN ('running', 'waiting')`)
	return err
}

// ListActiveAgentSessions returns all running/waiting sessions.
func (s *Store) ListActiveAgentSessions() ([]*model.AgentSession, error) {
	rows, err := s.db.Query(`
		SELECT id, ticket_id, pid, started_at, state, log_path
		FROM agent_sessions
		WHERE state IN ('running', 'waiting')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []*model.AgentSession
	for rows.Next() {
		sess, err := scanAgentSession(rows)
		if err != nil {
			return nil, err
		}
		if sess != nil {
			sessions = append(sessions, sess)
		}
	}
	return sessions, rows.Err()
}

func scanAgentSession(r scanner) (*model.AgentSession, error) {
	var (
		sess      model.AgentSession
		startedMs int64
		state     string
	)
	err := r.Scan(&sess.ID, &sess.TicketID, &sess.PID, &startedMs, &state, &sess.LogPath)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sess.StartedAt = time.UnixMilli(startedMs)
	sess.State = model.AgentState(state)
	return &sess, nil
}
