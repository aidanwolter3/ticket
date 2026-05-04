package store

import (
	"fmt"
	"time"

	"github.com/aidanwolter/ticket/internal/ids"
	"github.com/aidanwolter/ticket/internal/model"
)

func (s *Store) AddNote(ticketID, author, text string) (*model.Note, error) {
	id := ids.NewUUID()
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO notes (id, ticket_id, author, text, created) VALUES (?, ?, ?, ?, ?)`,
		id, ticketID, author, text, now)
	if err != nil {
		return nil, fmt.Errorf("add note: %w", err)
	}
	return &model.Note{
		ID:       id,
		TicketID: ticketID,
		Author:   author,
		Text:     text,
		Created:  time.UnixMilli(now),
	}, nil
}

func (s *Store) GetNotesForTicket(ticketID string) ([]*model.Note, error) {
	rows, err := s.db.Query(`SELECT id, ticket_id, author, text, created FROM notes WHERE ticket_id=? ORDER BY created`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notes []*model.Note
	for rows.Next() {
		var (
			n         model.Note
			createdMs int64
		)
		if err := rows.Scan(&n.ID, &n.TicketID, &n.Author, &n.Text, &createdMs); err != nil {
			return nil, err
		}
		n.Created = time.UnixMilli(createdMs)
		notes = append(notes, &n)
	}
	return notes, rows.Err()
}
