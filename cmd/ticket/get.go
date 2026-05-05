package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func runGet(args []string, defaultDB string) {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket get [--db path] <id>")
		os.Exit(1)
	}
	id := fs.Arg(0)

	s := openStore(*dbPath)
	defer s.Close()

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

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(tj)
}
