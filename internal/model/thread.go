package model

import (
	"strings"
	"time"
)

type ThreadStatus string

const (
	ThreadOpen           ThreadStatus = "open"
	ThreadNeedsAttention ThreadStatus = "needs_attention"
	ThreadResolved       ThreadStatus = "resolved"
)

type Thread struct {
	ID         string
	TaskID     string
	Status     ThreadStatus
	FilePath   string
	HunkHeader string
	Messages   []Message
	Created    time.Time
}

func (t *Thread) Summary() string {
	if len(t.Messages) == 0 {
		return "(empty thread)"
	}
	msg := strings.SplitN(t.Messages[0].Text, "\n", 2)[0]
	if len([]rune(msg)) > 60 {
		return string([]rune(msg)[:60]) + "…"
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
