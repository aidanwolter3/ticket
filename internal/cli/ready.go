package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/workflow"
)

func RunReady(args []string, defaultDB string) {
	s, fs := parseAndOpen(string(model.StatusReady), args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket ready [--db path] <ticket-id> <author>")
		fmt.Fprintln(os.Stderr, "  author: human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := workflow.Promote(s, ticketID, nil, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → ready\n", ticketID)
}
