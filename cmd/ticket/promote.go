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
		fmt.Fprintln(os.Stderr, "usage: ticket promote [--db path] <plan-id> <author>")
		fmt.Fprintln(os.Stderr, "  author: human:<name>")
		os.Exit(1)
	}
	planID := fs.Arg(0)
	author := fs.Arg(1)

	s := openStore(*dbPath)
	defer s.Close()

	promoted, err := s.PromoteDraftChildren(planID, author)
	if err != nil {
		fmt.Fprintf(os.Stderr, "promote failed: %v\n", err)
		os.Exit(1)
	}

	if len(promoted) == 0 {
		fmt.Println("no draft tickets to promote")
		return
	}
	for _, t := range promoted {
		fmt.Printf("%s → ready\n", t.ID)
	}
}
