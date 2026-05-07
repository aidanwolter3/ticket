package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

func RunGet(args []string, defaultDB string) {
	var jsonOut *bool
	s, fs := parseAndOpen("get", args, defaultDB, func(f *flag.FlagSet) {
		jsonOut = f.Bool("json", false, "output raw JSON")
	})
	defer s.Close()

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket get [--db path] [--json] <id>")
		os.Exit(1)
	}
	id := fs.Arg(0)

	t, err := s.GetTicket(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	tj := toTicketJSON(t)

	// Load threads per task.
	for i := range tj.Tasks {
		taskID := tj.Tasks[i].ID
		threads, err := s.GetThreadsForTask(taskID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load threads for task %s: %v\n", taskID, err)
			os.Exit(1)
		}
		for _, th := range threads {
			tj.Tasks[i].Threads = append(tj.Tasks[i].Threads, toThreadJSON(th))
		}
	}

	notes, err := s.GetNotesForTicket(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load notes: %v\n", err)
		os.Exit(1)
	}
	for _, n := range notes {
		tj.Notes = append(tj.Notes, noteJSON{
			ID: n.ID, Author: n.Author, Text: n.Text, Created: n.Created,
		})
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(tj)
		return
	}

	blockedBy := "none"
	if len(tj.BlockedBy) > 0 {
		blockedBy = strings.Join(tj.BlockedBy, ", ")
	}

	fmt.Printf("ID:             %s\n", tj.ID)
	fmt.Printf("Title:          %s\n", tj.Title)
	fmt.Printf("Type:           %s\n", tj.Type)
	fmt.Printf("Status:         %s\n", tj.Status)
	if tj.FeatureBranch != "" {
		fmt.Printf("Feature Branch: %s\n", tj.FeatureBranch)
	}
	fmt.Printf("Blocked By:     %s\n", blockedBy)

	if len(tj.Tasks) > 0 {
		fmt.Println("\nTasks:")
		for _, task := range tj.Tasks {
			check := " "
			if task.CompletedAt != nil {
				check = "x"
			}
			fmt.Printf("  %d. [%s] %s  %s\n", task.Position, check, task.ID, task.Title)
		}
	}
}
