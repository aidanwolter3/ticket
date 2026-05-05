package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aidanwolter/ticket/internal/workflow"
)

func runMerge(args []string, defaultDB string) {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket merge [--db path] <id> <author>")
		fmt.Fprintln(os.Stderr, "  author must be human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)

	if author != "human" && !strings.HasPrefix(author, "human:") {
		fmt.Fprintf(os.Stderr, "merge: only humans may merge tickets (got %q)\n", author)
		os.Exit(1)
	}

	s := openStore(*dbPath)
	defer s.Close()

	if err := workflow.Merge(s, ticketID); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → merged\n", ticketID)
}
