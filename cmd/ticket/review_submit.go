package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/workflow"
)

func runReviewSubmit(args []string, defaultDB string) {
	fs := flag.NewFlagSet("review-submit", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket review-submit [--db path] <ticket-id> <author>")
		fmt.Fprintln(os.Stderr, "  author must be human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)

	s := openStore(*dbPath)
	defer s.Close()

	// Snapshot active threads before the transition so we can report them.
	threads, err := s.GetThreadsForTicket(ticketID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "review-submit: %v\n", err)
		os.Exit(1)
	}
	var openIDs []string
	for _, th := range threads {
		if th.Status == model.ThreadOpen {
			openIDs = append(openIDs, th.ID)
		}
	}

	if err := workflow.ReviewSubmit(s, ticketID, author, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "review-submit: %v\n", err)
		os.Exit(1)
	}

	for _, id := range openIDs {
		fmt.Printf("  thread %s → needs_attention\n", id)
	}
	fmt.Printf("%s → ready\n", ticketID)
}
