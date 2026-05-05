package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
)

func runTask(args []string, defaultDB string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket task <subcommand>")
		fmt.Fprintln(os.Stderr, "  ls         <ticket-id>")
		fmt.Fprintln(os.Stderr, "  complete   <task-id> <author>")
		fmt.Fprintln(os.Stderr, "  uncomplete <task-id> <author>")
		os.Exit(1)
	}
	switch args[0] {
	case "ls":
		runTaskList(args[1:], defaultDB)
	case "complete":
		runTaskComplete(args[1:], defaultDB, false)
	case "uncomplete":
		runTaskComplete(args[1:], defaultDB, true)
	default:
		fmt.Fprintf(os.Stderr, "unknown task subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runTaskList(args []string, defaultDB string) {
	fs := flag.NewFlagSet("task ls", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	jsonOut := fs.Bool("json", false, "output tasks as JSON")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket task ls [--db path] [--json] <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	s := openStore(*dbPath)
	defer s.Close()

	tasks, err := s.GetTasksForTicket(ticketID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get tasks: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		out := make([]taskJSON, 0, len(tasks))
		for _, t := range tasks {
			out = append(out, toTaskJSON(&t))
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	if len(tasks) == 0 {
		fmt.Println("no tasks")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, t := range tasks {
		status := "○"
		if t.CompletedAt != nil {
			status = "✓"
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n", t.ID, t.Position, status, t.Title, t.CommitHash)
	}
	w.Flush()
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
