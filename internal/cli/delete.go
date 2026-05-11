package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunDelete(args []string, defaultDB string) {
	s, fs := parseAndOpen("delete", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket delete [--db path] <id>")
		os.Exit(1)
	}
	id := fs.Arg(0)

	if err := human.Delete(s, id); err != nil {
		fmt.Fprintf(os.Stderr, "delete failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("deleted %s\n", id)
}
