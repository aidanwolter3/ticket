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

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket in-review [--db path] <ticket-id> <author>")
		fmt.Fprintln(os.Stderr, "  author: agent:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)

	if err := transitionInReview(s, ticketID, author); err != nil {
		fmt.Fprintf(os.Stderr, "in-review: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → in_review\n", ticketID)
}

func transitionInReview(s *store.Store, ticketID, author string) error {
	if err := s.TransitionTicket(ticketID, model.StatusInReview, author); err != nil {
		return err
	}
	return nil
}
