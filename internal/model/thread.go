package model

import "time"

type ThreadStatus string

const (
	ThreadOpen           ThreadStatus = "open"
	ThreadNeedsAttention ThreadStatus = "needs_attention"
	ThreadResolved       ThreadStatus = "resolved"
)

type Thread struct {
	ID       string
	TaskID   string
	Status   ThreadStatus
	Messages []Message
	Created  time.Time
}

func (t *Thread) Summary() string {
	if len(t.Messages) == 0 {
		return "(empty thread)"
	}
	msg := t.Messages[0].Text
	if len(msg) > 60 {
		return msg[:60] + "…"
	}
	return msg
}

type Message struct {
	ID       string
	ThreadID string
	Author   string
	Text     string
	Created  time.Time
}
