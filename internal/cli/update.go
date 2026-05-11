package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunUpdate(args []string, wf *human.Workflow) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(os.Stderr, "usage: ticket update <ticket-id> [--title <title>] [--description <desc>]")
		os.Exit(1)
	}
	ticketID := args[0]

	fs := flag.NewFlagSet("update", flag.ExitOnError)
	title := fs.String("title", "", "new ticket title")
	description := fs.String("description", "", "new ticket description")
	fs.Parse(args[1:])

	titleSet := *title != ""
	descSet := *description != ""
	if !titleSet && !descSet {
		fmt.Fprintln(os.Stderr, "error: at least one of --title or --description must be provided")
		os.Exit(1)
	}

	var titlePtr, descPtr *string
	if titleSet {
		titlePtr = title
	}
	if descSet {
		descPtr = description
	}

	if err := wf.Update(ticketID, titlePtr, descPtr); err != nil {
		fmt.Fprintf(os.Stderr, "update ticket: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s updated\n", ticketID)
}
