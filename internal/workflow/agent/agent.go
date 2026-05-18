package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

// StartWork transitions a ticket from ready to in_progress.
func StartWork(s *store.Store, ticketID string) error {
	if err := s.TransitionTicket(ticketID, model.StatusInProgress); err != nil {
		return fmt.Errorf("start-work: %w", err)
	}
	return nil
}

// SubmitForReview transitions a ticket from in_progress to in_review.
func SubmitForReview(s *store.Store, ticketID string) error {
	if err := s.TransitionTicket(ticketID, model.StatusInReview); err != nil {
		return fmt.Errorf("submit-for-review: %w", err)
	}
	return nil
}

// AddNote adds a note to a ticket and returns the new note's ID.
func AddNote(s *store.Store, ticketID, author, text string) (string, error) {
	note, err := s.AddNote(ticketID, author, text)
	if err != nil {
		return "", fmt.Errorf("add note: %w", err)
	}
	return note.ID, nil
}

// CompleteTask marks a task complete. If the task requires a commit (NoCommit == false),
// commitHash must be non-empty. Pass an empty string for no-commit tasks.
func CompleteTask(s *store.Store, taskID, commitHash string) error {
	task, err := s.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("complete task: %w", err)
	}
	if task.NoCommit || commitHash == "" {
		if err := s.CompleteTask(taskID); err != nil {
			return fmt.Errorf("complete task: %w", err)
		}
	} else {
		if err := s.CompleteTaskWithCommit(taskID, commitHash); err != nil {
			return fmt.Errorf("complete task: %w", err)
		}
	}
	return nil
}

// CompleteTaskMostRecentCommit resolves the HEAD commit from the ticket's worktree
// (or repo_path) and calls CompleteTask with that hash.
func CompleteTaskMostRecentCommit(s *store.Store, taskID string) error {
	task, err := s.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("complete task: %w", err)
	}
	ticket, err := s.GetTicket(task.TicketID)
	if err != nil {
		return fmt.Errorf("complete task: get ticket: %w", err)
	}
	gitPath := ticket.WorktreePath
	if gitPath == "" {
		gitPath = ticket.RepoPath
	}
	if gitPath == "" {
		return fmt.Errorf("complete task: ticket has no worktree_path or repo_path set")
	}
	out, err := exec.Command("git", "-C", gitPath, "rev-parse", "HEAD").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "complete task: git rev-parse HEAD: %v (no commit hash recorded)\n", err)
		return CompleteTask(s, taskID, "")
	}
	return CompleteTask(s, taskID, strings.TrimSpace(string(out)))
}

// UncompleteTask marks a previously-completed task as incomplete.
func UncompleteTask(s *store.Store, taskID string) error {
	if err := s.UncompleteTask(taskID); err != nil {
		return fmt.Errorf("uncomplete task: %w", err)
	}
	return nil
}
