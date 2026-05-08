package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/workflow"
)

const workTypeAmendment = store.WorkTypeAmendment

type workOutputItem struct {
	Type         store.WorkType `json:"type"`
	Ticket       ticketWorkJSON `json:"ticket"`
	Instructions string         `json:"instructions"`
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

Before transitioning back to in_review, verify commit cleanliness:
1. No commits with 'fixup!' or 'squash!' prefixes — eliminate by amending or rebasing.
2. Review the full commit log with judgment: if a commit touches only a handful of lines and clearly extends a sibling task's commit, fold it in.
3. Exactly one commit per task, message format '<ticket-id> <task-id>: <task-title>'.
4. Feature branch rebased onto current main — no divergence.
5. No merge commits in the branch history.
6. Every ready thread has been replied to and transitioned back to active.`

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

func RunClaimWork(args []string, defaultDB string) {
	var jsonOut *bool
	s, _ := parseAndOpen("claim-work", args, defaultDB, func(f *flag.FlagSet) {
		jsonOut = f.Bool("json", false, "output raw JSON")
	})
	defer s.Close()

	item, err := workflow.Claim(s, "agent:claude", os.Stderr, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "claim-work: %v\n", err)
		os.Exit(1)
	}
	if item == nil {
		if *jsonOut {
			fmt.Println("null")
		} else {
			fmt.Println("no work available")
		}
		return
	}

	t := item.Ticket
	tw := buildTicketWorkJSON(s, item)

	instructions := newWorkInstructions
	if item.Type == workTypeAmendment {
		instructions = amendmentInstructions
	}

	out := workOutputItem{
		Type:         item.Type,
		Ticket:       tw,
		Instructions: instructions,
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, item.Type, t.Title)
	w.Flush()
}

func RunPeekWork(args []string, defaultDB string) {
	var jsonOut *bool
	s, _ := parseAndOpen("peek-work", args, defaultDB, func(f *flag.FlagSet) {
		jsonOut = f.Bool("json", false, "output raw JSON")
	})
	defer s.Close()

	items, err := s.PeekWork()
	if err != nil {
		fmt.Fprintf(os.Stderr, "peek-work: %v\n", err)
		os.Exit(1)
	}

	if len(items) == 0 {
		if *jsonOut {
			fmt.Println("[]")
		} else {
			fmt.Println("no work available")
		}
		return
	}

	if *jsonOut {
		out := make([]workOutputItem, 0, len(items))
		for _, item := range items {
			instructions := newWorkInstructions
			if item.Type == workTypeAmendment {
				instructions = amendmentInstructions
			}
			out = append(out, workOutputItem{
				Type:         item.Type,
				Ticket:       buildTicketWorkJSON(s, item),
				Instructions: instructions,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, item := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\n", item.Ticket.ID, item.Type, item.Ticket.Title)
	}
	w.Flush()
}
