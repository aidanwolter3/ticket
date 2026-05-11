package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunRedraft(args []string, defaultDB string) {
	s, fs := parseAndOpen("redraft", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket redraft [--db path] <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := human.Redraft(s, ticketID, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "redraft: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → draft\n", ticketID)
}
