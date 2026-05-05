package main

import (
	"flag"
	"fmt"
	"os"
)

func runPromote(args []string, defaultDB string) {
	fs := flag.NewFlagSet("promote", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket promote [--db path] <ticket-id> <author>")
		fmt.Fprintln(os.Stderr, "  author: human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)

	s := openStore(*dbPath)
	defer s.Close()

	if err := s.PromoteTicket(ticketID, author); err != nil {
		fmt.Fprintf(os.Stderr, "promote failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → ready\n", ticketID)
}
