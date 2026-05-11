package workflow

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

// AddTask creates a new task on the given ticket.
func AddTask(s *store.Store, ticketID, title, description, verifiableResult string, noCommit bool) (*model.Task, error) {
	position := 1
	last, err := s.LastTaskForTicket(ticketID)
	if err != nil {
		return nil, fmt.Errorf("add task: last task: %w", err)
	}
	if last != nil {
		position = last.Position + 1
	}

	task := &model.Task{
		TicketID:         ticketID,
		Title:            title,
		Description:      description,
		VerifiableResult: verifiableResult,
		NoCommit:         noCommit,
		Position:         position,
	}
	if err := s.CreateTask(task); err != nil {
		return nil, fmt.Errorf("add task: %w", err)
	}
	return task, nil
}

// UpdateTask updates the title, description, verifiable result, or no-commit flag of a task.
// Pass nil pointers to leave fields unchanged.
func UpdateTask(s *store.Store, taskID string, title, description, verifiableResult *string, noCommit *bool) error {
	task, err := s.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	if title != nil {
		task.Title = *title
	}
	if description != nil {
		task.Description = *description
	}
	if verifiableResult != nil {
		task.VerifiableResult = *verifiableResult
	}
	if noCommit != nil {
		task.NoCommit = *noCommit
	}
	if err := s.UpdateTask(task); err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	return nil
}

// DeleteTask deletes a task by ID.
func DeleteTask(s *store.Store, taskID string) error {
	if err := s.DeleteTask(taskID); err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return nil
}

// MoveTask moves a task to the given position.
func MoveTask(s *store.Store, taskID string, position int) error {
	if err := s.MoveTask(taskID, position); err != nil {
		return fmt.Errorf("move task: %w", err)
	}
	return nil
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
		return fmt.Errorf("complete task: git rev-parse HEAD: %w", err)
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
