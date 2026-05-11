package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunInReview(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("in-review", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket in-review <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := wf.SubmitForReview(ticketID); err != nil {
		fmt.Fprintf(os.Stderr, "in-review: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → in_review\n", ticketID)
}
