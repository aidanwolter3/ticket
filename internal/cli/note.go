package cli

import (
	"flag"
	"fmt"
	"os"
)

func RunNote(args []string, defaultDB string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket note <subcommand>")
		fmt.Fprintln(os.Stderr, "  add <ticket-id> <author> <text>")
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		runNoteAdd(args[1:], defaultDB)
	default:
		fmt.Fprintf(os.Stderr, "unknown note subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runNoteAdd(args []string, defaultDB string) {
	fs := flag.NewFlagSet("note add", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 3 {
		fmt.Fprintln(os.Stderr, "usage: ticket note add [--db path] <ticket-id> <author> <text>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)
	text := fs.Arg(2)

	s := openStore(*dbPath)
	defer s.Close()

	note, err := s.AddNote(ticketID, author, text)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note add failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(note.ID)
}
