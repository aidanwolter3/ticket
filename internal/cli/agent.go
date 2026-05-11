package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunAgent(args []string, wf *human.Workflow) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket agent <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands: clear")
		os.Exit(1)
	}
	switch args[0] {
	case "clear":
		runAgentClear(args[1:], wf)
	default:
		fmt.Fprintf(os.Stderr, "unknown agent subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runAgentClear(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("agent clear", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket agent clear <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	if err := wf.DeleteAgentSessionsByTicket(ticketID); err != nil {
		fmt.Fprintf(os.Stderr, "clear failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("cleared agent sessions for %s\n", ticketID)
}
