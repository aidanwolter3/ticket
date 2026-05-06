package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/aidanwolter/ticket/internal/workflow"
)

func runClaimWork(args []string, defaultDB string) {
	fs := flag.NewFlagSet("claim-work", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	jsonOut := fs.Bool("json", false, "output raw JSON")
	fs.Parse(args)

	s := openStore(*dbPath)
	defer s.Close()

	item, err := workflow.Claim(s, "agent:claude", os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "claim-work: %v\n", err)
		os.Exit(1)
	}
	if item == nil {
		if *jsonOut {
			fmt.Println("null")
		} else {
			fmt.Println("no work available")
		}
		return
	}

	t := item.Ticket
	tw := buildTicketWorkJSON(s, item)

	instructions := newWorkInstructions
	if item.Type == workTypeAmendment {
		instructions = amendmentInstructions
	}

	out := workOutputItem{
		Type:         item.Type,
		Ticket:       tw,
		Instructions: instructions,
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, item.Type, t.Title)
	w.Flush()
}

func runPeekWork(args []string, defaultDB string) {
	fs := flag.NewFlagSet("peek-work", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	jsonOut := fs.Bool("json", false, "output raw JSON")
	fs.Parse(args)

	s := openStore(*dbPath)
	defer s.Close()

	items, err := s.PeekWork()
	if err != nil {
		fmt.Fprintf(os.Stderr, "peek-work: %v\n", err)
		os.Exit(1)
	}

	if len(items) == 0 {
		if *jsonOut {
			fmt.Println("[]")
		} else {
			fmt.Println("no work available")
		}
		return
	}

	if *jsonOut {
		out := make([]workOutputItem, 0, len(items))
		for _, item := range items {
			instructions := newWorkInstructions
			if item.Type == workTypeAmendment {
				instructions = amendmentInstructions
			}
			out = append(out, workOutputItem{
				Type:         item.Type,
				Ticket:       buildTicketWorkJSON(s, item),
				Instructions: instructions,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, item := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\n", item.Ticket.ID, item.Type, item.Ticket.Title)
	}
	w.Flush()
}
