package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunDelete(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket delete <id>")
		os.Exit(1)
	}
	id := fs.Arg(0)

	if err := wf.Delete(id); err != nil {
		fmt.Fprintf(os.Stderr, "delete failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("deleted %s\n", id)
}
