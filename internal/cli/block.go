package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow"
)

func RunBlock(args []string, defaultDB string) {
	s, fs := parseAndOpen("block", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket block [--db path] <ticket-id> <blocker-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	blockerID := fs.Arg(1)

	if err := workflow.BlockTicket(s, ticketID, blockerID); err != nil {
		fmt.Fprintf(os.Stderr, "block failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s blocked by %s\n", ticketID, blockerID)
}

func RunUnblock(args []string, defaultDB string) {
	s, fs := parseAndOpen("unblock", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket unblock [--db path] <ticket-id> <blocker-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	blockerID := fs.Arg(1)

	if err := workflow.UnblockTicket(s, ticketID, blockerID); err != nil {
		fmt.Fprintf(os.Stderr, "unblock failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s unblocked from %s\n", ticketID, blockerID)
}
