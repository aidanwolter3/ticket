package model

import "time"

const (
	DraftActionResolve = "resolve"
	DraftActionReopen  = "reopen"
)

type DraftThread struct {
	ID         string
	TicketID   string
	TaskID     string
	FilePath   string
	HunkHeader string
	Messages   []DraftMessage
	Created    time.Time
}

type DraftMessage struct {
	ID           string
	ThreadID     string
	TicketID     string
	IsRealThread bool
	Author       string
	Text         string
	Created      time.Time
}

type DraftAction struct {
	ThreadID string
	TicketID string
	Action   string
	Created  time.Time
}

type DraftState struct {
	TicketID   string
	NewThreads []DraftThread
	Replies    []DraftMessage
	Actions    []DraftAction
}

func (s *DraftState) IsEmpty() bool {
	return len(s.NewThreads) == 0 && len(s.Replies) == 0 && len(s.Actions) == 0
}

func (s *DraftState) ActionFor(threadID string) string {
	for _, a := range s.Actions {
		if a.ThreadID == threadID {
			return a.Action
		}
	}
	return ""
}

func (s *DraftState) RepliesFor(threadID string) []DraftMessage {
	var out []DraftMessage
	for _, r := range s.Replies {
		if r.ThreadID == threadID {
			out = append(out, r)
		}
	}
	return out
}
