package model

import "time"

type Status string

const (
	StatusDraft      Status = "draft"
	StatusReady      Status = "ready"
	StatusInProgress Status = "in_progress"
	StatusInReview   Status = "in_review"
	StatusApproved   Status = "approved"
	StatusMerged     Status = "merged"
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
	Backlog       bool
	FeatureBranch string
	WorktreePath  string
	RepoPath      string
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
	Round            int // 1 = original work; N = Nth amendment cycle
	NoCommit         bool
	CommitHash       string
	VerifiableResult string
	CompletedAt      *time.Time
	Threads          []Thread
	Created          time.Time
	Updated          time.Time
}
