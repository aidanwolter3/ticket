package cli

import (
	"flag"
	"fmt"
	"os"
)

func RunBlock(args []string, defaultDB string) {
	fs := flag.NewFlagSet("block", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket block [--db path] <ticket-id> <blocker-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	blockerID := fs.Arg(1)

	s := openStore(*dbPath)
	defer s.Close()

	if err := s.AddBlocker(ticketID, blockerID); err != nil {
		fmt.Fprintf(os.Stderr, "block failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s blocked by %s\n", ticketID, blockerID)
}

func RunUnblock(args []string, defaultDB string) {
	fs := flag.NewFlagSet("unblock", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket unblock [--db path] <ticket-id> <blocker-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	blockerID := fs.Arg(1)

	s := openStore(*dbPath)
	defer s.Close()

	if err := s.RemoveBlocker(ticketID, blockerID); err != nil {
		fmt.Fprintf(os.Stderr, "unblock failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s unblocked from %s\n", ticketID, blockerID)
}
