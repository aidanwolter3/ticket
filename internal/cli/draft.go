package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunDraft(args []string, defaultDB string) {
	var title, description, repo *string
	var jsonOut *bool
	s, _ := parseAndOpen(string(model.StatusDraft), args, defaultDB, func(f *flag.FlagSet) {
		title = f.String("title", "", "ticket title (required)")
		description = f.String("description", "", "ticket description (use - to read from stdin)")
		repo = f.String("repo", "", "repository path (required)")
		jsonOut = f.Bool("json", false, "output full ticket JSON instead of just the ID")
	})
	defer s.Close()

	if *title == "" || *repo == "" {
		fmt.Fprintln(os.Stderr, "error: --title and --repo are required")
		fmt.Fprintln(os.Stderr, "usage: ticket draft --title STR --repo STR [--description STR|-] [--json]")
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

	t, err := human.Draft(s, *title, desc, *repo)
	if err != nil {
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
