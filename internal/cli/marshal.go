package cli

import (
	"time"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

const workTypeAmendment = store.WorkTypeAmendment

const newWorkInstructions = `This is a new ticket. Work through the tasks in order:

1. Implement the changes described in each task.
2. If the task has a verifiable_result, run it and confirm it passes.
3. Commit with message "<ticket-id>: <task-title>" — amend the commit if you need to revise.
4. Move on to the next task. Each task should produce exactly one commit on the feature branch.
5. When all tasks are complete, add a note summarising any non-obvious decisions, then transition the ticket to in_review.`

const amendmentInstructions = `This ticket was submitted for review but received feedback that needs to be addressed.

Walk through the tasks below in order. For each task:
1. Read the ready threads attached to it — these are the feedback items to address.
2. Make the necessary changes.
3. If the task has a verifiable_result, run it and confirm it passes.
4. Amend the task's existing commit (do not create a new one). Rebase subsequent task commits on top.
5. For each ready thread you addressed: reply with a brief description of the fix, then transition the thread from ready → active.

Before transitioning back to in_review, verify commit cleanliness:
1. No commits with 'fixup!' or 'squash!' prefixes — eliminate by amending or rebasing.
2. Review the full commit log with judgment: if a commit touches only a handful of lines and clearly extends a sibling task's commit, fold it in.
3. Exactly one commit per task, message format '<ticket-id> <task-id>: <task-title>'.
4. Feature branch rebased onto current main — no divergence.
5. No merge commits in the branch history.
6. Every ready thread has been replied to and transitioned back to active.`

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

// ticketWorkJSON is the trimmed ticket shape returned by find-work and claim-work.
// It intentionally omits Type, RepoPath, TaskCount, CompletedTaskCount, top-level
// Threads, Created, and Updated — those fields are not needed by agents picking up work.
type ticketWorkJSON struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	Status        string     `json:"status"`
	FeatureBranch string     `json:"feature_branch,omitempty"`
	WorktreePath  string     `json:"worktree_path,omitempty"`
	BlockedBy     []string   `json:"blocked_by,omitempty"`
	Tasks         []taskJSON `json:"tasks"`
	Notes         []noteJSON `json:"notes,omitempty"`
}

// workOutputItem is the JSON shape returned by find-work and claim-work.
type workOutputItem struct {
	Type         store.WorkType `json:"type"`
	Ticket       ticketWorkJSON `json:"ticket"`
	Instructions string         `json:"instructions"`
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

// buildTicketWorkJSON converts a WorkItem into its JSON representation.
func buildTicketWorkJSON(s *store.Store, item *store.WorkItem) ticketWorkJSON {
	t := item.Ticket
	tw := ticketWorkJSON{
		ID:            t.ID,
		Title:         t.Title,
		Description:   t.Description,
		Status:        string(t.Status),
		FeatureBranch: t.FeatureBranch,
		WorktreePath:  t.WorktreePath,
		BlockedBy:     t.BlockedBy,
	}
	for _, task := range t.Tasks {
		threads, err := s.GetThreadsForTask(task.ID)
		if err != nil {
			threads = nil
		}
		tj := taskJSON{
			ID:               task.ID,
			Position:         task.Position,
			Title:            task.Title,
			Description:      task.Description,
			CommitHash:       task.CommitHash,
			VerifiableResult: task.VerifiableResult,
			CompletedAt:      task.CompletedAt,
		}
		for _, th := range threads {
			if item.Type == workTypeAmendment {
				tj.Threads = append(tj.Threads, toThreadJSON(th))
			}
		}
		tw.Tasks = append(tw.Tasks, tj)
	}
	notes, _ := s.GetNotesForTicket(t.ID)
	for _, n := range notes {
		tw.Notes = append(tw.Notes, noteJSON{
			ID: n.ID, Author: n.Author, Text: n.Text, Created: n.Created,
		})
	}
	return tw
}
