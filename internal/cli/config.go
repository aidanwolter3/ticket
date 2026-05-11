package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunConfig(args []string, wf *human.Workflow) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ticket config <set|get|ls> [args...]")
		os.Exit(1)
	}
	switch args[0] {
	case "set":
		runConfigSet(args[1:], wf)
	case "get":
		runConfigGet(args[1:], wf)
	case "ls":
		runConfigList(args[1:], wf)
	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runConfigSet(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("config set", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket config set <key> <value>")
		os.Exit(1)
	}
	key := fs.Arg(0)
	value := fs.Arg(1)

	if key == "agent.command" && !strings.Contains(value, "{}") {
		fmt.Fprintln(os.Stderr, "config set: agent.command must contain '{}' as the prompt placeholder")
		os.Exit(1)
	}

	if err := wf.ConfigSet(key, value); err != nil {
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

func runConfigList(args []string, wf *human.Workflow) {
	flag.NewFlagSet("config ls", flag.ExitOnError).Parse(args)

	stored, err := wf.ConfigList()
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

func runConfigGet(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("config get", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket config get <key>")
		os.Exit(1)
	}
	key := fs.Arg(0)

	// Apply defaults for known keys.
	defaults := map[string]string{
		"worktrees": "true",
	}

	value, ok, err := wf.ConfigGet(key)
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
