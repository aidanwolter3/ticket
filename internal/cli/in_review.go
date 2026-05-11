package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

func RunInReview(args []string, defaultDB string) {
	s, fs := parseAndOpen(string(model.StatusInReview), args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket in-review [--db path] <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := transitionInReview(s, ticketID); err != nil {
		fmt.Fprintf(os.Stderr, "in-review: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → in_review\n", ticketID)
}

func transitionInReview(s *store.Store, ticketID string) error {
	if err := s.TransitionTicket(ticketID, model.StatusInReview); err != nil {
		return err
	}
	return nil
}
