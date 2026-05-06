package model

import "time"

type AgentState string

const (
	AgentRunning    AgentState = "running"
	AgentWaiting    AgentState = "waiting"
	AgentTerminated AgentState = "terminated"
	AgentCrashed    AgentState = "crashed"
)

type AgentSession struct {
	ID        string
	TicketID  string
	PID       int
	StartedAt time.Time
	State     AgentState
	LogPath   string
}
