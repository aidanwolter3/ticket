package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func runApprove(args []string, defaultDB string) {
	fs := flag.NewFlagSet("approve", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket approve [--db path] <id> <author>")
		fmt.Fprintln(os.Stderr, "  author must be human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)

	if author != "human" && !strings.HasPrefix(author, "human:") {
		fmt.Fprintf(os.Stderr, "approve: only humans may approve tickets (got %q)\n", author)
		os.Exit(1)
	}

	s := openStore(*dbPath)
	defer s.Close()

	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "approve: %v\n", err)
		os.Exit(1)
	}

	if ticket.Status != "in_review" {
		fmt.Fprintf(os.Stderr, "approve: ticket %s is %s, not in_review\n", ticketID, ticket.Status)
		os.Exit(1)
	}

	// Check for open threads.
	for _, task := range ticket.Tasks {
		threads, err := s.GetThreadsForTask(task.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "approve: load threads: %v\n", err)
			os.Exit(1)
		}
		for _, th := range threads {
			if th.Status == "active" || th.Status == "ready" {
				fmt.Fprintf(os.Stderr, "approve: ticket %s has open thread %s (status: %s) — resolve all threads before approving\n",
					ticketID, th.ID, th.Status)
				os.Exit(1)
			}
		}
	}

	if err := s.TransitionTicket(ticketID, "approved", author); err != nil {
		fmt.Fprintf(os.Stderr, "approve: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → approved\n", ticketID)
}
