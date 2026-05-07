package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow"
)

func RunRedraft(args []string, defaultDB string) {
	s, fs := parseAndOpen("redraft", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket redraft [--db path] <ticket-id> <author>")
		fmt.Fprintln(os.Stderr, "  author must be human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)

	if err := workflow.Redraft(s, ticketID, author, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "redraft: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → draft\n", ticketID)
}
