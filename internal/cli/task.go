package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/aidanwolter/ticket/internal/model"
)

func RunTask(args []string, defaultDB string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket task <subcommand>")
		fmt.Fprintln(os.Stderr, "  add        <ticket-id> --title <title> [--description <desc>] [--verifiable-result <vr>]")
		fmt.Fprintln(os.Stderr, "  ls         <ticket-id>")
		fmt.Fprintln(os.Stderr, "  complete   <task-id> <author>")
		fmt.Fprintln(os.Stderr, "  uncomplete <task-id> <author>")
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		runTaskAdd(args[1:], defaultDB)
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

func runTaskAdd(args []string, defaultDB string) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(os.Stderr, "usage: ticket task add [--db path] <ticket-id> --title <title> [--description <desc>] [--verifiable-result <vr>]")
		os.Exit(1)
	}
	ticketID := args[0]

	fs := flag.NewFlagSet("task add", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	title := fs.String("title", "", "task title (required)")
	description := fs.String("description", "", "task description")
	verifiableResult := fs.String("verifiable-result", "", "verifiable result")
	fs.Parse(args[1:])

	if *title == "" {
		fmt.Fprintln(os.Stderr, "error: --title is required")
		os.Exit(1)
	}

	s := openStore(*dbPath)
	defer s.Close()

	position := 1
	last, err := s.LastTaskForTicket(ticketID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get last task: %v\n", err)
		os.Exit(1)
	}
	if last != nil {
		position = last.Position + 1
	}

	task := &model.Task{
		TicketID:         ticketID,
		Title:            *title,
		Description:      *description,
		VerifiableResult: *verifiableResult,
		Position:         position,
	}
	if err := s.CreateTask(task); err != nil {
		fmt.Fprintf(os.Stderr, "create task: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(task.ID)
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
