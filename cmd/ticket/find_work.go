package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aidanwolter/ticket/internal/store"
)

// workOutputItem is the JSON shape returned by find-work.
type workOutputItem struct {
	Type         store.WorkType   `json:"type"`
	Ticket       ticketWorkJSON   `json:"ticket"`
	Instructions string           `json:"instructions"`
}

type ticketWorkJSON struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Description   string         `json:"description,omitempty"`
	Status        string         `json:"status"`
	FeatureBranch string         `json:"feature_branch,omitempty"`
	WorktreePath  string         `json:"worktree_path,omitempty"`
	BlockedBy     []string       `json:"blocked_by,omitempty"`
	Tasks         []taskWorkJSON `json:"tasks"`
	Notes         []noteJSON     `json:"notes,omitempty"`
}

type taskWorkJSON struct {
	ID               string       `json:"id"`
	Position         int          `json:"position"`
	Title            string       `json:"title"`
	Description      string       `json:"description,omitempty"`
	CommitHash       string       `json:"commit_hash,omitempty"`
	VerifiableResult string       `json:"verifiable_result,omitempty"`
	CompletedAt      *time.Time   `json:"completed_at,omitempty"`
	Threads          []threadJSON `json:"threads,omitempty"`
}

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

When all ready threads across all tasks have been addressed, transition the ticket back to in_review.`

func runFindWork(args []string, defaultDB string) {
	fs := flag.NewFlagSet("find-work", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	s := openStore(*dbPath)
	defer s.Close()

	items, err := s.FindWork()
	if err != nil {
		fmt.Fprintf(os.Stderr, "find-work: %v\n", err)
		os.Exit(1)
	}

	if len(items) == 0 {
		fmt.Println("[]")
		return
	}

	out := make([]workOutputItem, 0, len(items))
	for _, item := range items {
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

		// For each task, load its threads (only ready threads for amendments).
		for _, task := range t.Tasks {
			threads, err := s.GetThreadsForTask(task.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "load threads for task %s: %v\n", task.ID, err)
				os.Exit(1)
			}

			tj := taskWorkJSON{
				ID:               task.ID,
				Position:         task.Position,
				Title:            task.Title,
				Description:      task.Description,
				CommitHash:       task.CommitHash,
				VerifiableResult: task.VerifiableResult,
				CompletedAt:      task.CompletedAt,
			}
			for _, th := range threads {
				// For new work, omit threads. For amendments, include all threads.
				if item.Type == store.WorkTypeAmendment {
					tj.Threads = append(tj.Threads, toThreadJSON(th))
				}
			}
			tw.Tasks = append(tw.Tasks, tj)
		}

		notes, err := s.GetNotesForTicket(t.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load notes for %s: %v\n", t.ID, err)
			os.Exit(1)
		}
		for _, n := range notes {
			tw.Notes = append(tw.Notes, noteJSON{
				ID: n.ID, Author: n.Author, Text: n.Text, Created: n.Created,
			})
		}

		instructions := newWorkInstructions
		if item.Type == store.WorkTypeAmendment {
			instructions = amendmentInstructions
		}

		out = append(out, workOutputItem{
			Type:         item.Type,
			Ticket:       tw,
			Instructions: instructions,
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}
