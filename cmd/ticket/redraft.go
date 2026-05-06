package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow"
)

func runRedraft(args []string, defaultDB string) {
	fs := flag.NewFlagSet("redraft", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket redraft [--db path] <ticket-id> <author>")
		fmt.Fprintln(os.Stderr, "  author must be human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)

	s := openStore(*dbPath)
	defer s.Close()

	if err := workflow.Redraft(s, ticketID, author, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "redraft: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → draft\n", ticketID)
}
