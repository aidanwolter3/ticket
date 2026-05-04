package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
)

func runThread(args []string, defaultDB string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket thread <subcommand>")
		fmt.Fprintln(os.Stderr, "  reply <thread-id> <author> <text>")
		fmt.Fprintln(os.Stderr, "  transition <thread-id> <new-status> <author>")
		os.Exit(1)
	}
	switch args[0] {
	case "reply":
		runThreadReply(args[1:], defaultDB)
	case "transition":
		runThreadTransition(args[1:], defaultDB)
	default:
		fmt.Fprintf(os.Stderr, "unknown thread subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runThreadReply(args []string, defaultDB string) {
	fs := flag.NewFlagSet("thread reply", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 3 {
		fmt.Fprintln(os.Stderr, "usage: ticket thread reply [--db path] <thread-id> <author> <text>")
		os.Exit(1)
	}
	threadID := fs.Arg(0)
	author := fs.Arg(1)
	text := fs.Arg(2)

	s := openStore(*dbPath)
	defer s.Close()

	msg, err := s.AddMessage(threadID, author, text)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thread reply failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(msg.ID)
}

func runThreadTransition(args []string, defaultDB string) {
	fs := flag.NewFlagSet("thread transition", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 3 {
		fmt.Fprintln(os.Stderr, "usage: ticket thread transition [--db path] <thread-id> <new-status> <author>")
		fmt.Fprintln(os.Stderr, "  agents may only transition: ready → active")
		os.Exit(1)
	}
	threadID := fs.Arg(0)
	newStatus := model.ThreadStatus(fs.Arg(1))
	author := fs.Arg(2)

	s := openStore(*dbPath)
	defer s.Close()

	if err := s.TransitionThread(threadID, newStatus, author); err != nil {
		fmt.Fprintf(os.Stderr, "thread transition failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s → %s\n", threadID, newStatus)
}
