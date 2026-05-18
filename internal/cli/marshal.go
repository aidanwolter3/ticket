package cli

import (
	"time"

	"github.com/aidanwolter/ticket/internal/model"
)

// ticketJSON is the full JSON representation of a ticket (used by ls, get, draft).
type ticketJSON struct {
	ID                 string       `json:"id"`
	Title              string       `json:"title"`
	Type               string       `json:"type"`
	Status             string       `json:"status"`
	Description        string       `json:"description,omitempty"`
	FeatureBranch      string       `json:"feature_branch,omitempty"`
	WorktreePath       string       `json:"worktree_path,omitempty"`
	RepoPath           string       `json:"repo_path,omitempty"`
	Config             string       `json:"config,omitempty"`
	BlockedBy          []string     `json:"blocked_by,omitempty"`
	TaskCount          int          `json:"task_count"`
	CompletedTaskCount int          `json:"completed_task_count"`
	Tasks              []taskJSON   `json:"tasks,omitempty"`
	Threads            []threadJSON `json:"threads,omitempty"`
	Notes              []noteJSON   `json:"notes,omitempty"`
	Created            time.Time    `json:"created"`
	Updated            time.Time    `json:"updated"`
}

// taskJSON is the canonical task representation used by all JSON outputs.
// Created and Updated use pointers so that work-output callers can omit them
// without affecting the ls/get/task output (which always sets them).
type taskJSON struct {
	ID               string       `json:"id"`
	Position         int          `json:"position"`
	Title            string       `json:"title"`
	Description      string       `json:"description,omitempty"`
	NoCommit         bool         `json:"no_commit,omitempty"`
	CommitHash       string       `json:"commit_hash,omitempty"`
	VerifiableResult string       `json:"verifiable_result,omitempty"`
	CompletedAt      *time.Time   `json:"completed_at,omitempty"`
	Threads          []threadJSON `json:"threads,omitempty"`
	Created          *time.Time   `json:"created,omitempty"`
	Updated          *time.Time   `json:"updated,omitempty"`
}

type threadJSON struct {
	ID       string        `json:"id"`
	TaskID   string        `json:"task_id"`
	Status   string        `json:"status"`
	Messages []messageJSON `json:"messages,omitempty"`
	Created  time.Time     `json:"created"`
}

type messageJSON struct {
	ID      string    `json:"id"`
	Author  string    `json:"author"`
	Text    string    `json:"text"`
	Created time.Time `json:"created"`
}

type noteJSON struct {
	ID      string    `json:"id"`
	Author  string    `json:"author"`
	Text    string    `json:"text"`
	Created time.Time `json:"created"`
}

func toTicketJSON(t *model.Ticket) ticketJSON {
	completed := 0
	for _, task := range t.Tasks {
		if task.CompletedAt != nil {
			completed++
		}
	}
	tj := ticketJSON{
		ID:                 t.ID,
		Title:              t.Title,
		Type:               string(t.Type),
		Status:             string(t.Status),
		Description:        t.Description,
		FeatureBranch:      t.FeatureBranch,
		WorktreePath:       t.WorktreePath,
		RepoPath:           t.RepoPath,
		Config:             t.Config,
		BlockedBy:          t.BlockedBy,
		TaskCount:          len(t.Tasks),
		CompletedTaskCount: completed,
		Created:            t.Created,
		Updated:            t.Updated,
	}
	for _, task := range t.Tasks {
		tj.Tasks = append(tj.Tasks, toTaskJSON(&task))
	}
	for _, n := range t.Notes {
		tj.Notes = append(tj.Notes, noteJSON{
			ID:      n.ID,
			Author:  n.Author,
			Text:    n.Text,
			Created: n.Created,
		})
	}
	return tj
}

func toTaskJSON(t *model.Task) taskJSON {
	tj := taskJSON{
		ID:               t.ID,
		Position:         t.Position,
		Title:            t.Title,
		Description:      t.Description,
		NoCommit:         t.NoCommit,
		CommitHash:       t.CommitHash,
		VerifiableResult: t.VerifiableResult,
		CompletedAt:      t.CompletedAt,
		Created:          &t.Created,
		Updated:          &t.Updated,
	}
	for _, th := range t.Threads {
		tj.Threads = append(tj.Threads, toThreadJSON(&th))
	}
	return tj
}

func toThreadJSON(th *model.Thread) threadJSON {
	tj := threadJSON{
		ID:      th.ID,
		TaskID:  th.TaskID,
		Status:  string(th.Status),
		Created: th.Created,
	}
	for _, m := range th.Messages {
		tj.Messages = append(tj.Messages, messageJSON{
			ID:      m.ID,
			Author:  m.Author,
			Text:    m.Text,
			Created: m.Created,
		})
	}
	return tj
}
