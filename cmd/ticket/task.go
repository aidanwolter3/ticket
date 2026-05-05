package main

import (
	"flag"
	"fmt"
	"os"
)

func runTask(args []string, defaultDB string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket task <subcommand>")
		fmt.Fprintln(os.Stderr, "  complete   <task-id> <author>")
		fmt.Fprintln(os.Stderr, "  uncomplete <task-id> <author>")
		os.Exit(1)
	}
	switch args[0] {
	case "complete":
		runTaskComplete(args[1:], defaultDB, false)
	case "uncomplete":
		runTaskComplete(args[1:], defaultDB, true)
	default:
		fmt.Fprintf(os.Stderr, "unknown task subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runTaskComplete(args []string, defaultDB string, undo bool) {
	subCmd := "complete"
	if undo {
		subCmd = "uncomplete"
	}
	fs := flag.NewFlagSet("task "+subCmd, flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "usage: ticket task %s [--db path] <task-id>\n", subCmd)
		os.Exit(1)
	}
	taskID := fs.Arg(0)

	s := openStore(*dbPath)
	defer s.Close()

	var err error
	if undo {
		err = s.UncompleteTask(taskID)
	} else {
		err = s.CompleteTask(taskID)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "task %s failed: %v\n", subCmd, err)
		os.Exit(1)
	}
	fmt.Printf("%s → %sd\n", taskID, subCmd)
}
