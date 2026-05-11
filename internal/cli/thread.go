package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/workflow"
)

func RunThread(args []string, defaultDB string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket thread <subcommand>")
		fmt.Fprintln(os.Stderr, "  reply <thread-id> <author> <text>")
		fmt.Fprintln(os.Stderr, "  transition <thread-id> <new-status> <author>")
		os.Exit(1)
	}
	switch args[0] {
	case "reply":
		runThreadReply(args[1:], defaultDB)
	case "transition":
		runThreadTransition(args[1:], defaultDB)
	default:
		fmt.Fprintf(os.Stderr, "unknown thread subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runThreadReply(args []string, defaultDB string) {
	s, fs := parseAndOpen("thread reply", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 3 {
		fmt.Fprintln(os.Stderr, "usage: ticket thread reply [--db path] <thread-id> <author> <text>")
		os.Exit(1)
	}
	threadID := fs.Arg(0)
	author := fs.Arg(1)
	text := fs.Arg(2)

	msg, err := workflow.ReplyToThread(s, threadID, author, text)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thread reply failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(msg.ID)
}

func runThreadTransition(args []string, defaultDB string) {
	s, fs := parseAndOpen("thread transition", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket thread transition [--db path] <thread-id> <new-status> [author]")
		os.Exit(1)
	}
	threadID := fs.Arg(0)
	newStatus := model.ThreadStatus(fs.Arg(1))
	author := fs.Arg(2)

	if err := workflow.TransitionThread(s, threadID, newStatus, author); err != nil {
		fmt.Fprintf(os.Stderr, "thread transition failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s → %s\n", threadID, newStatus)
}
