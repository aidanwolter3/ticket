package model

import "time"

type Status string

const (
	StatusDraft      Status = "draft"
	StatusReady      Status = "ready"
	StatusInProgress Status = "in_progress"
	StatusInReview   Status = "in_review"
	StatusCompleted  Status = "completed"
)

type TicketType string

const (
	TypeTicket TicketType = "ticket"
)

type Ticket struct {
	ID            string
	Title         string
	Description   string
	Type          TicketType
	Status        Status
	FeatureBranch string
	WorktreePath  string
	BlockedBy     []string
	Tasks         []Task
	Threads       []Thread // aggregated from all tasks
	Notes         []Note
	Created       time.Time
	Updated       time.Time
}

type Task struct {
	ID               string
	TicketID         string
	Title            string
	Description      string
	Position         int
	CommitHash       string
	VerifiableResult string
	CompletedAt      *time.Time
	Threads          []Thread
	Created          time.Time
	Updated          time.Time
}
