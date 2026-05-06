package cli

import (
	"flag"
	"fmt"
	"os"
)

func RunDelete(args []string, defaultDB string) {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket delete [--db path] <id>")
		os.Exit(1)
	}
	id := fs.Arg(0)

	s := openStore(*dbPath)
	defer s.Close()

	if err := s.DeleteTicket(id); err != nil {
		fmt.Fprintf(os.Stderr, "delete failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("deleted %s\n", id)
}
