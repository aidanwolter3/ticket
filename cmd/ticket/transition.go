package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/workflow"
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

	var transErr error
	if status == model.StatusReady {
		transErr = workflow.Ready(s, id, author, os.Stdout, os.Stderr)
	} else {
		transErr = s.TransitionTicket(id, status, author)
	}
	if transErr != nil {
		fmt.Fprintf(os.Stderr, "transition failed: %v\n", transErr)
		os.Exit(1)
	}

	fmt.Printf("%s → %s\n", id, status)
}
