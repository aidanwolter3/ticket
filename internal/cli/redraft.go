package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunRedraft(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("redraft", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket redraft <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := wf.Redraft(ticketID, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "redraft: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → draft\n", ticketID)
}
