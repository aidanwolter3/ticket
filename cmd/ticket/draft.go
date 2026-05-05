package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
)

func runDraft(args []string, defaultDB string) {
	fs := flag.NewFlagSet("draft", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	title := fs.String("title", "", "ticket title (required)")
	description := fs.String("description", "", "ticket description (use - to read from stdin)")
	branch := fs.String("branch", "", "feature branch name")
	repo := fs.String("repo", "", "repository path (required)")
	jsonOut := fs.Bool("json", false, "output full ticket JSON instead of just the ID")
	fs.Parse(args)

	if *title == "" || *repo == "" {
		fmt.Fprintln(os.Stderr, "error: --title and --repo are required")
		fmt.Fprintln(os.Stderr, "usage: ticket draft --title STR --repo STR [--description STR|-] [--branch STR] [--json]")
		os.Exit(1)
	}

	desc := *description
	if desc == "-" {
		scanner := bufio.NewScanner(os.Stdin)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
			os.Exit(1)
		}
		desc = strings.Join(lines, "\n")
	}

	s := openStore(*dbPath)
	defer s.Close()

	t := &model.Ticket{
		Title:         *title,
		Type:          model.TypeTicket,
		Status:        model.StatusDraft,
		Description:   desc,
		FeatureBranch: *branch,
		RepoPath:      *repo,
	}
	if err := s.CreateTicket(t); err != nil {
		fmt.Fprintf(os.Stderr, "create ticket: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		tj := toTicketJSON(t)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(tj)
		return
	}

	fmt.Println(t.ID)
}
