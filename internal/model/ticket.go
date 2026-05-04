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

// DerivePlanStatus computes a plan's status from its children's statuses.
// Rules (priority order):
//  1. All completed → completed
//  2. Any in_progress → in_progress
//  3. Any in_review (none in_progress) → in_review
//  4. Any ready (none of the above) → ready
//  5. Otherwise → draft
func DerivePlanStatus(childStatuses []Status) Status {
	if len(childStatuses) == 0 {
		return StatusDraft
	}
	allCompleted := true
	hasInProgress := false
	hasInReview := false
	hasReady := false
	for _, s := range childStatuses {
		if s != StatusCompleted {
			allCompleted = false
		}
		switch s {
		case StatusInProgress:
			hasInProgress = true
		case StatusInReview:
			hasInReview = true
		case StatusReady:
			hasReady = true
		}
	}
	switch {
	case allCompleted:
		return StatusCompleted
	case hasInProgress:
		return StatusInProgress
	case hasInReview:
		return StatusInReview
	case hasReady:
		return StatusReady
	default:
		return StatusDraft
	}
}
