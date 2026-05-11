package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunTask(args []string, wf *human.Workflow) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket task <subcommand>")
		fmt.Fprintln(os.Stderr, "  add    <ticket-id> --title <title> [--description <desc>] [--verifiable-result <vr>] [--no-commit]")
		fmt.Fprintln(os.Stderr, "  get    [--json] <task-id>")
		fmt.Fprintln(os.Stderr, "  ls     [--json] <ticket-id>")
		fmt.Fprintln(os.Stderr, "  update <task-id> [--title <title>] [--description <desc>] [--verifiable-result <vr>]")
		fmt.Fprintln(os.Stderr, "  move   <task-id> <position>")
		fmt.Fprintln(os.Stderr, "  delete <task-id>")
		fmt.Fprintln(os.Stderr, "(agent-only: complete, uncomplete, set-commit — use ticket --agent task <subcommand>)")
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		runTaskAdd(args[1:], wf)
	case "get":
		runTaskGet(args[1:], wf)
	case "ls":
		runTaskList(args[1:], wf)
	case "update":
		runTaskUpdate(args[1:], wf)
	case "move":
		runTaskMove(args[1:], wf)
	case "delete":
		runTaskDelete(args[1:], wf)
	default:
		fmt.Fprintf(os.Stderr, "unknown task subcommand: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "note: complete, uncomplete, set-commit require 'ticket --agent task %s'\n", args[0])
		os.Exit(1)
	}
}

// RunAgentTask handles agent-only task subcommands (complete, uncomplete, set-commit).
func RunAgentTask(args []string, wf *human.Workflow) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket --agent task <subcommand>")
		fmt.Fprintln(os.Stderr, "  complete   [--most-recent-commit | --commit <hash>] <task-id>")
		fmt.Fprintln(os.Stderr, "  uncomplete <task-id>")
		fmt.Fprintln(os.Stderr, "  set-commit <task-id> <hash>")
		os.Exit(1)
	}
	switch args[0] {
	case "complete":
		runTaskComplete(args[1:], wf, false)
	case "uncomplete":
		runTaskComplete(args[1:], wf, true)
	case "set-commit":
		runTaskSetCommit(args[1:], wf)
	default:
		fmt.Fprintf(os.Stderr, "unknown agent task subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runTaskAdd(args []string, wf *human.Workflow) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(os.Stderr, "usage: ticket task add <ticket-id> --title <title> [--description <desc>] [--verifiable-result <vr>] [--no-commit]")
		os.Exit(1)
	}
	ticketID := args[0]

	fs := flag.NewFlagSet("task add", flag.ExitOnError)
	title := fs.String("title", "", "task title (required)")
	description := fs.String("description", "", "task description")
	verifiableResult := fs.String("verifiable-result", "", "verifiable result")
	noCommit := fs.Bool("no-commit", false, "task produces no commit (e.g. verification-only)")
	fs.Parse(args[1:])

	if *title == "" {
		fmt.Fprintln(os.Stderr, "error: --title is required")
		os.Exit(1)
	}

	task, err := wf.AddTask(ticketID, *title, *description, *verifiableResult, *noCommit, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create task: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(task.ID)
}

func runTaskGet(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("task get", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output task as JSON")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket task get [--json] <task-id>")
		os.Exit(1)
	}
	taskID := fs.Arg(0)

	task, err := wf.GetTask(taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	threads, err := wf.GetThreadsForTask(taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load threads: %v\n", err)
		os.Exit(1)
	}

	tj := toTaskJSON(task)
	for _, th := range threads {
		tj.Threads = append(tj.Threads, toThreadJSON(th))
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(tj)
		return
	}

	status := "incomplete"
	if task.CompletedAt != nil {
		status = "complete"
	}
	fmt.Printf("ID:               %s\n", tj.ID)
	fmt.Printf("Title:            %s\n", tj.Title)
	fmt.Printf("Position:         %d\n", tj.Position)
	fmt.Printf("Status:           %s\n", status)
	if tj.Description != "" {
		fmt.Printf("Description:      %s\n", tj.Description)
	}
	if tj.VerifiableResult != "" {
		fmt.Printf("Verifiable Result: %s\n", tj.VerifiableResult)
	}
	if tj.CommitHash != "" {
		fmt.Printf("Commit Hash:      %s\n", tj.CommitHash)
	}

	if len(tj.Threads) > 0 {
		fmt.Println("\nThreads:")
		for _, th := range tj.Threads {
			fmt.Printf("  %s  [%s]\n", th.ID, th.Status)
			for _, m := range th.Messages {
				fmt.Printf("    %s: %s\n", m.Author, m.Text)
			}
		}
	}
}

func runTaskList(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("task ls", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output tasks as JSON")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket task ls [--json] <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

	tasks, err := wf.GetTasksForTicket(ticketID)
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

func runTaskDelete(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("task delete", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket task delete <task-id>")
		os.Exit(1)
	}
	taskID := fs.Arg(0)

	if err := wf.DeleteTask(taskID); err != nil {
		fmt.Fprintf(os.Stderr, "delete task: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s deleted\n", taskID)
}

func runTaskComplete(args []string, wf *human.Workflow, undo bool) {
	subCmd := "complete"
	if undo {
		subCmd = "uncomplete"
	}

	fs := flag.NewFlagSet("task "+subCmd, flag.ExitOnError)
	var mostRecentCommit *bool
	var commitHash *string
	if !undo {
		mostRecentCommit = fs.Bool("most-recent-commit", false, "resolve HEAD commit hash from worktree_path (or repo_path) and record it")
		commitHash = fs.String("commit", "", "explicit commit hash to record")
	}
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "usage: ticket task %s <task-id>\n", subCmd)
		os.Exit(1)
	}
	taskID := fs.Arg(0)

	var err error
	if undo {
		err = wf.UncompleteTask(taskID)
	} else if *mostRecentCommit {
		err = wf.CompleteTaskMostRecentCommit(taskID)
	} else if *commitHash != "" {
		err = wf.CompleteTask(taskID, *commitHash)
	} else {
		task, getErr := wf.GetTask(taskID)
		if getErr != nil {
			fmt.Fprintf(os.Stderr, "get task: %v\n", getErr)
			os.Exit(1)
		}
		if task.NoCommit {
			err = wf.CompleteTask(taskID, "")
		} else {
			fmt.Fprintf(os.Stderr, "error: task %s requires a commit hash; use --commit <hash> or --most-recent-commit\n", taskID)
			fmt.Fprintf(os.Stderr, "       (use --no-commit on task add/update to mark a task as commit-free)\n")
			os.Exit(1)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "task %s failed: %v\n", subCmd, err)
		os.Exit(1)
	}
	fmt.Printf("%s → %sd\n", taskID, subCmd)
}

func runTaskUpdate(args []string, wf *human.Workflow) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(os.Stderr, "usage: ticket task update <task-id> [--title <title>] [--description <desc>] [--verifiable-result <vr>] [--no-commit]")
		os.Exit(1)
	}
	taskID := args[0]

	fs := flag.NewFlagSet("task update", flag.ExitOnError)
	title := fs.String("title", "", "new task title")
	description := fs.String("description", "", "new task description")
	verifiableResult := fs.String("verifiable-result", "", "new verifiable result")
	noCommit := fs.Bool("no-commit", false, "mark task as commit-free (verification-only)")
	fs.Parse(args[1:])

	titleSet := *title != ""
	descSet := *description != ""
	vrSet := *verifiableResult != ""
	noCommitSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "no-commit" {
			noCommitSet = true
		}
	})
	if !titleSet && !descSet && !vrSet && !noCommitSet {
		fmt.Fprintln(os.Stderr, "error: at least one of --title, --description, --verifiable-result, or --no-commit must be provided")
		os.Exit(1)
	}

	var titlePtr, descPtr, vrPtr *string
	var noCommitPtr *bool
	if titleSet {
		titlePtr = title
	}
	if descSet {
		descPtr = description
	}
	if vrSet {
		vrPtr = verifiableResult
	}
	if noCommitSet {
		noCommitPtr = noCommit
	}
	if err := wf.UpdateTask(taskID, titlePtr, descPtr, vrPtr, noCommitPtr); err != nil {
		fmt.Fprintf(os.Stderr, "update task: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s updated\n", taskID)
}

func runTaskSetCommit(args []string, wf *human.Workflow) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(os.Stderr, "usage: ticket task set-commit <task-id> <hash>")
		os.Exit(1)
	}
	taskID := args[0]

	fs := flag.NewFlagSet("task set-commit", flag.ExitOnError)
	fs.Parse(args[1:])

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket task set-commit <task-id> <hash>")
		os.Exit(1)
	}
	hash := fs.Arg(0)

	if err := wf.SetTaskCommit(taskID, hash); err != nil {
		fmt.Fprintf(os.Stderr, "update task: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s commit set to %s\n", taskID, hash)
}

func runTaskMove(args []string, wf *human.Workflow) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(os.Stderr, "usage: ticket task move <task-id> <position>")
		os.Exit(1)
	}
	taskID := args[0]

	fs := flag.NewFlagSet("task move", flag.ExitOnError)
	fs.Parse(args[1:])

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket task move <task-id> <position>")
		os.Exit(1)
	}

	var newPos int
	if _, err := fmt.Sscanf(fs.Arg(0), "%d", &newPos); err != nil || newPos < 1 {
		fmt.Fprintln(os.Stderr, "error: position must be a positive integer")
		os.Exit(1)
	}

	if err := wf.MoveTask(taskID, newPos); err != nil {
		fmt.Fprintf(os.Stderr, "move task: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s → position %d\n", taskID, newPos)
}
