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

	// If the first argument looks like a flag (starts with -), skip subcommand
	// dispatch and fall through to TUI with flag parsing.
	if len(os.Args) >= 2 && !strings.HasPrefix(os.Args[1], "-") {
		switch os.Args[1] {
		case "draft":
			cli.RunDraft(os.Args[2:], defaultDB)
		case "import":
			cli.RunImport(os.Args[2:], defaultDB)
		case "ls":
			cli.RunList(os.Args[2:], defaultDB)
		case "get":
			cli.RunGet(os.Args[2:], defaultDB)
		case "ready":
			cli.RunReady(os.Args[2:], defaultDB)
		case "redraft":
			cli.RunRedraft(os.Args[2:], defaultDB)
		case "delete":
			cli.RunDelete(os.Args[2:], defaultDB)
		case "purge":
			cli.RunPurge(os.Args[2:], defaultDB)
		case "note":
			cli.RunNote(os.Args[2:], defaultDB)
		case "thread":
			cli.RunThread(os.Args[2:], defaultDB)
		case "task":
			cli.RunTask(os.Args[2:], defaultDB)
		case "block":
			cli.RunBlock(os.Args[2:], defaultDB)
		case "unblock":
			cli.RunUnblock(os.Args[2:], defaultDB)
		case "config":
			cli.RunConfig(os.Args[2:], defaultDB)
		case "update":
			cli.RunUpdate(os.Args[2:], defaultDB)
		case "agent":
			cli.RunAgent(os.Args[2:], defaultDB)
		case "help", "--help", "-h":
			cli.PrintUsage()
		default:
			fmt.Fprintf(os.Stderr, "unknown command: %s\nRun 'ticket help' for usage.\n", os.Args[1])
			os.Exit(1)
		}
		return
	}

	// Launch TUI when no arguments are provided.
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

	app := tui.New(s)
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
	switch args[0] {
	case "in-progress":
		cli.RunInProgress(args[1:], defaultDB)
	case "in-review":
		cli.RunInReview(args[1:], defaultDB)
	case "task":
		cli.RunAgentTask(args[1:], defaultDB)
	default:
		fmt.Fprintf(os.Stderr, "unknown agent command: %s\nRun 'ticket --agent --help' for usage.\n", args[0])
		os.Exit(1)
	}
}
