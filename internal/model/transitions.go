package model

import (
	"fmt"
	"strings"
)

// ValidateTicketTransition returns an error if the status transition is not allowed.
// Pass author="human" or "human:<name>" for humans; "agent:<name>" for agents.
func ValidateTicketTransition(from, to Status, author string) error {
	isHuman := author == "human" || strings.HasPrefix(author, "human:")

	allowed := map[Status]map[Status]bool{
		StatusDraft:      {StatusReady: true},
		StatusReady:      {StatusDraft: true, StatusInProgress: true},
		StatusInProgress: {StatusInReview: true},
		StatusInReview:   {StatusReady: true, StatusApproved: true, StatusInProgress: true},
		StatusApproved:   {StatusMerged: true},
		StatusMerged:     {},
	}

	humanOnly := map[Status]map[Status]bool{
		StatusDraft:    {StatusReady: true},
		StatusReady:    {StatusDraft: true},
		StatusInReview: {StatusReady: true, StatusApproved: true},
		StatusApproved: {StatusMerged: true},
	}

	targets, ok := allowed[from]
	if !ok {
		return fmt.Errorf("unknown status %q", from)
	}
	if !targets[to] {
		// Special case: any → draft is a human override
		if to == StatusDraft && isHuman {
			return nil
		}
		return fmt.Errorf("invalid ticket transition: %s → %s", from, to)
	}
	if humanOnly[from][to] && !isHuman {
		return fmt.Errorf("transition %s → %s requires a human actor", from, to)
	}
	return nil
}

// ValidateThreadTransition returns an error if the thread status transition is not allowed.
func ValidateThreadTransition(from, to ThreadStatus, author string) error {
	isHuman := author == "human" || strings.HasPrefix(author, "human:")

	switch from {
	case ThreadActive:
		if to == ThreadReady {
			if !isHuman {
				return fmt.Errorf("transition active → ready requires a human actor")
			}
			return nil
		}
		if to == ThreadResolved {
			if !isHuman {
				return fmt.Errorf("transition active → resolved requires a human actor")
			}
			return nil
		}
	case ThreadReady:
		if to == ThreadActive {
			return nil // human or agent (agent posts amendment reply)
		}
		if to == ThreadResolved {
			if !isHuman {
				return fmt.Errorf("transition ready → resolved requires a human actor")
			}
			return nil
		}
	case ThreadResolved:
		if to == ThreadActive {
			if !isHuman {
				return fmt.Errorf("transition resolved → active requires a human actor")
			}
			return nil
		}
	}
	return fmt.Errorf("invalid thread transition: %s → %s", from, to)
}
