package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow"
)

func runReady(args []string, defaultDB string) {
	fs := flag.NewFlagSet("ready", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket ready [--db path] <ticket-id> <author>")
		fmt.Fprintln(os.Stderr, "  author: human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	s := openStore(*dbPath)
	defer s.Close()

	if err := workflow.Promote(s, ticketID, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → ready\n", ticketID)

	if t, err := s.GetTicket(ticketID); err == nil && t.WorktreePath != "" {
		fmt.Printf("worktree: %s (branch: %s)\n", t.WorktreePath, t.FeatureBranch)
	}
}
