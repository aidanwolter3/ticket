package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/aidanwolter/ticket/internal/workflow"
)

func RunMerge(args []string, defaultDB string) {
	s, fs := parseAndOpen("merge", args, defaultDB, nil)
	defer s.Close()

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

	if err := workflow.Merge(s, ticketID, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → merged\n", ticketID)
}
