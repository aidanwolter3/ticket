package workflow

import (
	"fmt"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

// ReplyToThread adds a message to a thread.
func ReplyToThread(s *store.Store, threadID, author, text string) error {
	if _, err := s.AddMessage(threadID, author, text); err != nil {
		return fmt.Errorf("reply to thread: %w", err)
	}
	return nil
}

// TransitionThread transitions a thread to a new status.
func TransitionThread(s *store.Store, threadID string, newStatus model.ThreadStatus, author string) error {
	if err := s.TransitionThread(threadID, newStatus, author); err != nil {
		return fmt.Errorf("transition thread: %w", err)
	}
	return nil
}

// BlockTicket adds a blocker to a ticket.
func BlockTicket(s *store.Store, ticketID, blockerID string) error {
	if err := s.AddBlocker(ticketID, blockerID); err != nil {
		return fmt.Errorf("block ticket: %w", err)
	}
	return nil
}

// UnblockTicket removes a blocker from a ticket.
func UnblockTicket(s *store.Store, ticketID, blockerID string) error {
	if err := s.RemoveBlocker(ticketID, blockerID); err != nil {
		return fmt.Errorf("unblock ticket: %w", err)
	}
	return nil
}
