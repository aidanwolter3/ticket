package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunReady(args []string, defaultDB string) {
	s, fs := parseAndOpen(string(model.StatusReady), args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket ready [--db path] <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := human.Promote(s, ticketID, nil, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → ready\n", ticketID)
}
