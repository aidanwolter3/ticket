package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
)

func runTransition(args []string, defaultDB string) {
	fs := flag.NewFlagSet("transition", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 3 {
		fmt.Fprintln(os.Stderr, "usage: ticket transition [--db path] <id> <status> <author>")
		fmt.Fprintln(os.Stderr, "  status: draft | ready | in_progress | in_review | completed")
		fmt.Fprintln(os.Stderr, "  author: human | agent:<name>")
		os.Exit(1)
	}
	id := fs.Arg(0)
	status := model.Status(fs.Arg(1))
	author := fs.Arg(2)

	s := openStore(*dbPath)
	defer s.Close()

	if err := s.TransitionTicket(id, status, author); err != nil {
		fmt.Fprintf(os.Stderr, "transition failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → %s\n", id, status)
}
