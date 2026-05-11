package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/aidanwolter/ticket/internal/model"
)

func RunTask(args []string, defaultDB string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket task <subcommand>")
		fmt.Fprintln(os.Stderr, "  add        <ticket-id> --title <title> [--description <desc>] [--verifiable-result <vr>]")
		fmt.Fprintln(os.Stderr, "  get        <task-id>")
		fmt.Fprintln(os.Stderr, "  ls         <ticket-id>")
		fmt.Fprintln(os.Stderr, "  update     <task-id> [--title <title>] [--description <desc>] [--verifiable-result <vr>]")
		fmt.Fprintln(os.Stderr, "  move       <task-id> <position>")
		fmt.Fprintln(os.Stderr, "  complete   <task-id> [--most-recent-commit] [--commit <hash>]")
		fmt.Fprintln(os.Stderr, "  uncomplete <task-id> <author>")
		fmt.Fprintln(os.Stderr, "  delete     <task-id>")
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		runTaskAdd(args[1:], defaultDB)
	case "get":
		runTaskGet(args[1:], defaultDB)
	case "ls":
		runTaskList(args[1:], defaultDB)
	case "update":
		runTaskUpdate(args[1:], defaultDB)
	case "move":
		runTaskMove(args[1:], defaultDB)
	case "complete":
		runTaskComplete(args[1:], defaultDB, false)
	case "uncomplete":
		runTaskComplete(args[1:], defaultDB, true)
	case "delete":
		runTaskDelete(args[1:], defaultDB)
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

	var title, description, verifiableResult *string
	s, _ := parseAndOpen("task add", args[1:], defaultDB, func(f *flag.FlagSet) {
		title = f.String("title", "", "task title (required)")
		description = f.String("description", "", "task description")
		verifiableResult = f.String("verifiable-result", "", "verifiable result")
	})
	defer s.Close()

	if *title == "" {
		fmt.Fprintln(os.Stderr, "error: --title is required")
		os.Exit(1)
	}

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

func runTaskGet(args []string, defaultDB string) {
	var jsonOut *bool
	s, fs := parseAndOpen("task get", args, defaultDB, func(f *flag.FlagSet) {
		jsonOut = f.Bool("json", false, "output task as JSON")
	})
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket task get [--db path] [--json] <task-id>")
		os.Exit(1)
	}
	taskID := fs.Arg(0)

	task, err := s.GetTask(taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	threads, err := s.GetThreadsForTask(taskID)
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

func runTaskList(args []string, defaultDB string) {
	var jsonOut *bool
	s, fs := parseAndOpen("task ls", args, defaultDB, func(f *flag.FlagSet) {
		jsonOut = f.Bool("json", false, "output tasks as JSON")
	})
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket task ls [--db path] [--json] <ticket-id>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)

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

func runTaskDelete(args []string, defaultDB string) {
	s, fs := parseAndOpen("task delete", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket task delete [--db path] <task-id>")
		os.Exit(1)
	}
	taskID := fs.Arg(0)

	if err := s.DeleteTask(taskID); err != nil {
		fmt.Fprintf(os.Stderr, "delete task: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s deleted\n", taskID)
}

func runTaskComplete(args []string, defaultDB string, undo bool) {
	subCmd := "complete"
	if undo {
		subCmd = "uncomplete"
	}

	var mostRecentCommit *bool
	var commitHash *string
	s, fs := parseAndOpen("task "+subCmd, args, defaultDB, func(f *flag.FlagSet) {
		if !undo {
			mostRecentCommit = f.Bool("most-recent-commit", false, "resolve HEAD commit hash from worktree_path (or repo_path) and record it")
			commitHash = f.String("commit", "", "explicit commit hash to record")
		}
	})
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "usage: ticket task %s [--db path] <task-id>\n", subCmd)
		os.Exit(1)
	}
	taskID := fs.Arg(0)

	var err error
	if undo {
		err = s.UncompleteTask(taskID)
	} else if commitHash != nil && *commitHash != "" {
		err = s.CompleteTaskWithCommit(taskID, *commitHash)
	} else if mostRecentCommit != nil && *mostRecentCommit {
		task, getErr := s.GetTask(taskID)
		if getErr != nil {
			fmt.Fprintf(os.Stderr, "get task: %v\n", getErr)
			os.Exit(1)
		}
		ticket, getErr := s.GetTicket(task.TicketID)
		if getErr != nil {
			fmt.Fprintf(os.Stderr, "get ticket: %v\n", getErr)
			os.Exit(1)
		}
		gitPath := ticket.WorktreePath
		if gitPath == "" {
			gitPath = ticket.RepoPath
		}
		if gitPath == "" {
			fmt.Fprintln(os.Stderr, "error: ticket has no worktree_path or repo_path set")
			os.Exit(1)
		}
		out, runErr := exec.Command("git", "-C", gitPath, "rev-parse", "HEAD").Output()
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "git rev-parse HEAD: %v\n", runErr)
			os.Exit(1)
		}
		err = s.CompleteTaskWithCommit(taskID, strings.TrimSpace(string(out)))
	} else {
		err = s.CompleteTask(taskID)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "task %s failed: %v\n", subCmd, err)
		os.Exit(1)
	}
	fmt.Printf("%s → %sd\n", taskID, subCmd)
}

func runTaskUpdate(args []string, defaultDB string) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(os.Stderr, "usage: ticket task update [--db path] <task-id> [--title <title>] [--description <desc>] [--verifiable-result <vr>]")
		os.Exit(1)
	}
	taskID := args[0]

	var title, description, verifiableResult *string
	s, _ := parseAndOpen("task update", args[1:], defaultDB, func(f *flag.FlagSet) {
		title = f.String("title", "", "new task title")
		description = f.String("description", "", "new task description")
		verifiableResult = f.String("verifiable-result", "", "new verifiable result")
	})
	defer s.Close()

	titleSet := *title != ""
	descSet := *description != ""
	vrSet := *verifiableResult != ""
	if !titleSet && !descSet && !vrSet {
		fmt.Fprintln(os.Stderr, "error: at least one of --title, --description, or --verifiable-result must be provided")
		os.Exit(1)
	}

	task, err := s.GetTask(taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if titleSet {
		task.Title = *title
	}
	if descSet {
		task.Description = *description
	}
	if vrSet {
		task.VerifiableResult = *verifiableResult
	}

	if err := s.UpdateTask(task); err != nil {
		fmt.Fprintf(os.Stderr, "update task: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s updated\n", taskID)
}

func runTaskMove(args []string, defaultDB string) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(os.Stderr, "usage: ticket task move [--db path] <task-id> <position>")
		os.Exit(1)
	}
	taskID := args[0]

	s, fs := parseAndOpen("task move", args[1:], defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket task move [--db path] <task-id> <position>")
		os.Exit(1)
	}

	var newPos int
	if _, err := fmt.Sscanf(fs.Arg(0), "%d", &newPos); err != nil || newPos < 1 {
		fmt.Fprintln(os.Stderr, "error: position must be a positive integer")
		os.Exit(1)
	}

	if err := s.MoveTask(taskID, newPos); err != nil {
		fmt.Fprintf(os.Stderr, "move task: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s → position %d\n", taskID, newPos)
}
