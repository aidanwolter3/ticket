package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/workflow"
)

func RunReviewSubmit(args []string, defaultDB string) {
	s, fs := parseAndOpen("review-submit", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket review-submit [--db path] <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	// Snapshot active threads before the transition so we can report them.
	threads, err := s.GetThreadsForTicket(ticketID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "review-submit: %v\n", err)
		os.Exit(1)
	}
	var openIDs []string
	for _, th := range threads {
		if th.Status == model.ThreadOpen {
			openIDs = append(openIDs, th.ID)
		}
	}

	if err := workflow.ReviewSubmit(s, ticketID, "human", os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "review-submit: %v\n", err)
		os.Exit(1)
	}

	for _, id := range openIDs {
		fmt.Printf("  thread %s → needs_attention\n", id)
	}
	fmt.Printf("%s → ready\n", ticketID)
}
