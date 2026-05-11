package human

import (
	"fmt"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

// AddTask creates a new task on the given ticket. Pass round=0 to use the default (1).
func AddTask(s *store.Store, ticketID, title, description, verifiableResult string, noCommit bool, round int) (*model.Task, error) {
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
		Round:            round,
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
