package cli

import (
	"flag"
	"fmt"
	"os"
)

func RunUpdate(args []string, defaultDB string) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(os.Stderr, "usage: ticket update [--db path] <ticket-id> [--title <title>] [--description <desc>]")
		os.Exit(1)
	}
	ticketID := args[0]

	var title, description *string
	s, _ := parseAndOpen("update", args[1:], defaultDB, func(f *flag.FlagSet) {
		title = f.String("title", "", "new ticket title")
		description = f.String("description", "", "new ticket description")
	})
	defer s.Close()

	titleSet := *title != ""
	descSet := *description != ""
	if !titleSet && !descSet {
		fmt.Fprintln(os.Stderr, "error: at least one of --title or --description must be provided")
		os.Exit(1)
	}

	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if titleSet {
		ticket.Title = *title
	}
	if descSet {
		ticket.Description = *description
	}

	if err := s.UpdateTicket(ticket); err != nil {
		fmt.Fprintf(os.Stderr, "update ticket: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s updated\n", ticketID)
}
