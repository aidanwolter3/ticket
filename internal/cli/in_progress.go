package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
)

func RunInProgress(args []string, defaultDB string) {
	s, fs := parseAndOpen(string(model.StatusInProgress), args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket in-progress [--db path] <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := s.TransitionTicket(ticketID, model.StatusInProgress, "agent:claude"); err != nil {
		fmt.Fprintf(os.Stderr, "in-progress: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → in_progress\n", ticketID)
}
