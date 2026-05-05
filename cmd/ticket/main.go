package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	home, _ := os.UserHomeDir()
	defaultDB := filepath.Join(home, ".local", "share", "ticket", "tickets.db")
	defaultLog := filepath.Join(home, ".local", "share", "ticket", "ticket.log")

	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "import":
			runImport(os.Args[2:], defaultDB)
			return
		case "ls":
			runList(os.Args[2:], defaultDB)
			return
		case "get":
			runGet(os.Args[2:], defaultDB)
			return
		case "find-work":
			runFindWork(os.Args[2:], defaultDB)
			return
		case "transition":
			runTransition(os.Args[2:], defaultDB)
			return
		case "promote":
			runPromote(os.Args[2:], defaultDB)
			return
		case "delete":
			runDelete(os.Args[2:], defaultDB)
			return
		case "purge":
			runPurge(os.Args[2:], defaultDB)
			return
		case "note":
			runNote(os.Args[2:], defaultDB)
			return
		case "thread":
			runThread(os.Args[2:], defaultDB)
			return
		case "task":
			runTask(os.Args[2:], defaultDB)
			return
		case "block":
			runBlock(os.Args[2:], defaultDB)
			return
		case "unblock":
			runUnblock(os.Args[2:], defaultDB)
			return
		case "help", "--help", "-h":
			printUsage()
			return
		}
	}

	// Default: launch TUI
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

func openStore(dbPath string) *store.Store {
	s, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open db: %v\n", err)
		os.Exit(1)
	}
	return s
}

func printUsage() {
	fmt.Print(`ticket — local-first ticket tracker

Usage:
  ticket [--db path]                          launch TUI
  ticket import [--db path] [file]            batch-create tickets from JSON (stdin if no file)
  ticket ls [--db path] [--status s] [--json] list tickets
  ticket get [--db path] <id>                 get a single ticket as JSON (includes tasks and threads)
  ticket find-work [--db path]                find actionable work for agents (JSON)
  ticket transition [--db path] <id> <status> <author>
                                              transition a ticket's status
  ticket note add [--db path] <ticket-id> <author> <text>
                                              add a note to a ticket
  ticket thread reply [--db path] <thread-id> <author> <text>
                                              add a reply to a thread
  ticket thread transition [--db path] <thread-id> <new-status> <author>
                                              transition a thread's status (agents: ready→active only)
  ticket task ls [--db path] [--json] <ticket-id>
                                              list tasks for a ticket
  ticket task complete [--db path] <task-id>  mark a task complete
  ticket task uncomplete [--db path] <task-id>
                                              mark a task incomplete
  ticket block [--db path] <ticket-id> <blocker-id>
                                              record that <ticket-id> is blocked by <blocker-id>
  ticket unblock [--db path] <ticket-id> <blocker-id>
                                              remove that dependency
  ticket promote [--db path] <ticket-id> <author>
                                              promote a draft ticket to ready
  ticket delete [--db path] <id>              delete a ticket
  ticket purge [--db path] --yes              delete the database file

For agent usage, see AGENTS.md.
`)
}
