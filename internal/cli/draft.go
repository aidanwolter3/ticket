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

func RunDraft(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet(string(model.StatusDraft), flag.ExitOnError)
	title := fs.String("title", "", "ticket title (required)")
	description := fs.String("description", "", "ticket description (use - to read from stdin)")
	repo := fs.String("repo", "", "repository path (required)")
	configName := fs.String("config", "", "named config to assign to this ticket")
	jsonOut := fs.Bool("json", false, "output full ticket JSON instead of just the ID")
	fs.Parse(args)

	if *title == "" || *repo == "" {
		fmt.Fprintln(os.Stderr, "error: --title and --repo are required")
		fmt.Fprintln(os.Stderr, "usage: ticket draft --title STR --repo STR [--description STR|-] [--config NAME] [--json]")
		os.Exit(1)
	}

	if *configName != "" {
		overrides, err := wf.NamedConfigList(*configName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "draft: %v\n", err)
			os.Exit(1)
		}
		if len(overrides) == 0 {
			fmt.Fprintf(os.Stderr, "error: named config %q does not exist (use ticket config set --config %s <key> <value> to create it)\n", *configName, *configName)
			os.Exit(1)
		}
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

	t, err := wf.Draft(*title, desc, *repo, *configName)
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
