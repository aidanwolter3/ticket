package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/aidanwolter/ticket/internal/cli"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui"
	"github.com/aidanwolter/ticket/internal/workflow/human"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	home, _ := os.UserHomeDir()
	defaultDB := filepath.Join(home, ".local", "share", "ticket", "tickets.db")
	defaultLog := filepath.Join(home, ".local", "share", "ticket", "ticket.log")

	// --agent mode: route to agent-only commands.
	if len(os.Args) >= 2 && os.Args[1] == "--agent" {
		runAgent(os.Args[2:], defaultDB)
		return
	}

	if len(os.Args) >= 2 && !strings.HasPrefix(os.Args[1], "-") {
		subCmd := os.Args[1]
		args := os.Args[2:]

		if subCmd == "help" || subCmd == "--help" || subCmd == "-h" {
			cli.PrintUsage()
			return
		}

		// purge deletes the db file; handle before opening store
		if subCmd == "purge" {
			dbPath := findFlag(args, "db", defaultDB)
			cli.RunPurge(stripFlag(args, "db"), dbPath)
			return
		}

		dbPath := findFlag(args, "db", defaultDB)
		s, err := store.Open(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open db: %v\n", err)
			os.Exit(1)
		}
		defer s.Close()
		wf := human.New(s)
		cleanArgs := stripFlag(args, "db")

		switch subCmd {
		case "draft":
			cli.RunDraft(cleanArgs, wf)
		case "import":
			cli.RunImport(cleanArgs, wf)
		case "ls":
			cli.RunList(cleanArgs, wf)
		case "get":
			cli.RunGet(cleanArgs, wf)
		case "ready":
			cli.RunReady(cleanArgs, wf)
		case "redraft":
			cli.RunRedraft(cleanArgs, wf)
		case "delete":
			cli.RunDelete(cleanArgs, wf)
		case "thread":
			cli.RunThread(cleanArgs, wf)
		case "task":
			cli.RunTask(cleanArgs, wf)
		case "block":
			cli.RunBlock(cleanArgs, wf)
		case "unblock":
			cli.RunUnblock(cleanArgs, wf)
		case "config":
			cli.RunConfig(cleanArgs, wf)
		case "update":
			cli.RunUpdate(cleanArgs, wf)
		case "agent":
			cli.RunAgent(cleanArgs, wf)
		case "note":
			cli.RunNote(cleanArgs, wf)
		case "backlog":
			cli.RunBacklog(cleanArgs, wf)
		case "unbacklog":
			cli.RunUnbacklog(cleanArgs, wf)
		default:
			fmt.Fprintf(os.Stderr, "unknown command: %s\nRun 'ticket help' for usage.\n", subCmd)
			os.Exit(1)
		}
		return
	}

	// Launch TUI when no subcommand is given.
	fs := flag.NewFlagSet("ticket", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(os.Args[1:])

	if err := os.MkdirAll(filepath.Dir(defaultLog), 0o755); err == nil {
		if lf, err := os.OpenFile(defaultLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			slog.SetDefault(slog.New(slog.NewTextHandler(lf, nil)))
			defer lf.Close()
		}
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open db: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	wf := human.New(s)
	app := tui.New(wf)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runAgent handles all agent-mode commands (ticket --agent <cmd> ...).
func runAgent(args []string, defaultDB string) {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		cli.PrintAgentUsage()
		return
	}

	subArgs := args[1:]
	dbPath := findFlag(subArgs, "db", defaultDB)
	s, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open db: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()
	wf := human.New(s)
	cleanArgs := stripFlag(subArgs, "db")

	switch args[0] {
	case "in-progress":
		cli.RunInProgress(cleanArgs, wf)
	case "in-review":
		cli.RunInReview(cleanArgs, wf)
	case "task":
		cli.RunAgentTask(cleanArgs, wf)
	case "note":
		cli.RunNote(cleanArgs, wf)
	default:
		fmt.Fprintf(os.Stderr, "unknown agent command: %s\nRun 'ticket --agent --help' for usage.\n", args[0])
		os.Exit(1)
	}
}

// findFlag scans args for --name value or --name=value and returns the value,
// falling back to defaultVal if not found.
func findFlag(args []string, name, defaultVal string) string {
	for i, a := range args {
		if a == "--"+name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, "--"+name+"=") {
			return strings.TrimPrefix(a, "--"+name+"=")
		}
	}
	return defaultVal
}

// stripFlag removes --name value or --name=value from args.
func stripFlag(args []string, name string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--"+name && i+1 < len(args) {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--"+name+"=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}
