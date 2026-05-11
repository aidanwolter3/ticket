package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunBlock(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("block", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket block <ticket-id> <blocker-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	blockerID := fs.Arg(1)

	if err := wf.BlockTicket(ticketID, blockerID); err != nil {
		fmt.Fprintf(os.Stderr, "block failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s blocked by %s\n", ticketID, blockerID)
}

func RunUnblock(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("unblock", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket unblock <ticket-id> <blocker-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	blockerID := fs.Arg(1)

	if err := wf.UnblockTicket(ticketID, blockerID); err != nil {
		fmt.Fprintf(os.Stderr, "unblock failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s unblocked from %s\n", ticketID, blockerID)
}
