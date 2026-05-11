package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunReady(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("ready", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket ready <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := wf.Ready(ticketID, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → ready\n", ticketID)
}
