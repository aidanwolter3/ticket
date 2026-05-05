package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
)

func runConfig(args []string, defaultDB string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket config <set|get|ls> [args...]")
		os.Exit(1)
	}
	switch args[0] {
	case "set":
		runConfigSet(args[1:], defaultDB)
	case "get":
		runConfigGet(args[1:], defaultDB)
	case "ls":
		runConfigList(args[1:], defaultDB)
	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runConfigSet(args []string, defaultDB string) {
	fs := flag.NewFlagSet("config set", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket config set [--db path] <key> <value>")
		os.Exit(1)
	}
	key := fs.Arg(0)
	value := fs.Arg(1)

	s := openStore(*dbPath)
	defer s.Close()

	if err := s.ConfigSet(key, value); err != nil {
		fmt.Fprintf(os.Stderr, "config set: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s = %s\n", key, value)
}

func runConfigList(args []string, defaultDB string) {
	fs := flag.NewFlagSet("config ls", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	s := openStore(*dbPath)
	defer s.Close()

	stored, err := s.ConfigList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config ls: %v\n", err)
		os.Exit(1)
	}

	defaults := map[string]string{
		"worktrees": "true",
	}
	merged := make(map[string]string)
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range stored {
		merged[k] = v
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s = %s\n", k, merged[k])
	}
}

func runConfigGet(args []string, defaultDB string) {
	fs := flag.NewFlagSet("config get", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket config get [--db path] <key>")
		os.Exit(1)
	}
	key := fs.Arg(0)

	s := openStore(*dbPath)
	defer s.Close()

	// Apply defaults for known keys.
	defaults := map[string]string{
		"worktrees": "true",
	}

	value, ok, err := s.ConfigGet(key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config get: %v\n", err)
		os.Exit(1)
	}
	if !ok {
		if def, hasDef := defaults[key]; hasDef {
			fmt.Println(def)
			return
		}
		fmt.Fprintf(os.Stderr, "config: key %q is not set\n", key)
		os.Exit(1)
	}
	fmt.Println(value)
}
