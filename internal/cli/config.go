package cli

import (
	"fmt"
	"os"
	"strings"
)

func RunConfig(args []string, defaultDB string) {
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
	s, fs := parseAndOpen("config set", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket config set [--db path] <key> <value>")
		os.Exit(1)
	}
	key := fs.Arg(0)
	value := fs.Arg(1)

	if key == "agent.command" && !strings.Contains(value, "{}") {
		fmt.Fprintln(os.Stderr, "config set: agent.command must contain '{}' as the prompt placeholder")
		os.Exit(1)
	}

	if err := s.ConfigSet(key, value); err != nil {
		fmt.Fprintf(os.Stderr, "config set: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s = %s\n", key, value)
}

// knownConfigKeys lists every supported config key with its default value (empty string = no default).
var knownConfigKeys = []struct {
	key        string
	defaultVal string
}{
	{"agent.auto_dispatch", ""},
	{"agent.command", ""},
	{"worktrees", "true"},
}

func runConfigList(args []string, defaultDB string) {
	s, _ := parseAndOpen("config ls", args, defaultDB, nil)
	defer s.Close()

	stored, err := s.ConfigList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config ls: %v\n", err)
		os.Exit(1)
	}

	for _, kd := range knownConfigKeys {
		if v, ok := stored[kd.key]; ok {
			fmt.Printf("%s = %s\n", kd.key, v)
		} else if kd.defaultVal != "" {
			fmt.Printf("%s = %s\n", kd.key, kd.defaultVal)
		} else {
			fmt.Printf("%s = <unset>\n", kd.key)
		}
	}
}

func runConfigGet(args []string, defaultDB string) {
	s, fs := parseAndOpen("config get", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket config get [--db path] <key>")
		os.Exit(1)
	}
	key := fs.Arg(0)

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
