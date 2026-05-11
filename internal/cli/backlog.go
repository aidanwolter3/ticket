package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunBacklog(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("backlog", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket backlog <id>")
		os.Exit(1)
	}
	id := fs.Arg(0)

	if err := wf.SetBacklog(id, true); err != nil {
		fmt.Fprintf(os.Stderr, "backlog failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → backlog\n", id)
}

func RunUnbacklog(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("unbacklog", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket unbacklog <id>")
		os.Exit(1)
	}
	id := fs.Arg(0)

	if err := wf.SetBacklog(id, false); err != nil {
		fmt.Fprintf(os.Stderr, "unbacklog failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → unbacklogged\n", id)
}
