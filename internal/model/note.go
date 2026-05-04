package model

import "time"

type Note struct {
	ID       string
	TicketID string
	Author   string
	Text     string
	Created  time.Time
}
