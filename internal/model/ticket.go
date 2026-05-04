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
	TypePlan   TicketType = "plan"
)

type Ticket struct {
	ID               string
	Title            string
	Description      string
	Type             TicketType
	Status           Status
	FeatureBranch    string
	StackID          string
	CommitHash       string
	VerifiableResult string
	BlockedBy        []string
	Threads          []Thread
	Notes            []Note
	Created          time.Time
	Updated          time.Time
}

func (t *Ticket) IsPlan() bool {
	return t.Type == TypePlan
}
