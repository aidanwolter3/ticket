package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/workflow/agent"
)

func RunInReview(args []string, defaultDB string) {
	s, fs := parseAndOpen(string(model.StatusInReview), args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket in-review [--db path] <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := agent.SubmitForReview(s, ticketID); err != nil {
		fmt.Fprintf(os.Stderr, "in-review: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → in_review\n", ticketID)
}
