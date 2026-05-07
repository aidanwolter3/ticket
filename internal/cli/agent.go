package cli

import (
	"flag"
	"fmt"
	"os"
)

func RunAgent(args []string, defaultDB string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket agent <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands: clear")
		os.Exit(1)
	}
	switch args[0] {
	case "clear":
		runAgentClear(args[1:], defaultDB)
	default:
		fmt.Fprintf(os.Stderr, "unknown agent subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runAgentClear(args []string, defaultDB string) {
	fs := flag.NewFlagSet("agent clear", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket agent clear [--db path] <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	s := openStore(*dbPath)
	defer s.Close()

	if err := s.DeleteAgentSessionsByTicket(ticketID); err != nil {
		fmt.Fprintf(os.Stderr, "clear failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("cleared agent sessions for %s\n", ticketID)
}
