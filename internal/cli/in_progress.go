package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunInProgress(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("in-progress", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket in-progress <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := wf.StartWork(ticketID); err != nil {
		fmt.Fprintf(os.Stderr, "in-progress: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → in_progress\n", ticketID)
}
