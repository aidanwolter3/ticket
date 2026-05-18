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
	configName := fs.String("config", "", "named config to scope the key under")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket config set [--config NAME] <key> <value>")
		os.Exit(1)
	}
	key := fs.Arg(0)
	value := fs.Arg(1)

	if key == "agent.command" && !strings.Contains(value, "{}") {
		fmt.Fprintln(os.Stderr, "config set: agent.command must contain '{}' as the prompt placeholder")
		os.Exit(1)
	}

	if *configName != "" {
		if err := wf.NamedConfigSet(*configName, key, value); err != nil {
			fmt.Fprintf(os.Stderr, "config set: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := wf.ConfigSet(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "config set: %v\n", err)
			os.Exit(1)
		}
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
	{"workspace.type", "worktree"},
	{"workspace.create_command", ""},
	{"workspace.delete_command", ""},
}

func runConfigList(args []string, wf *human.Workflow) {
	flag.NewFlagSet("config ls", flag.ExitOnError).Parse(args)

	stored, err := wf.ConfigList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config ls: %v\n", err)
		os.Exit(1)
	}

	// --- [default] section ---
	fmt.Println("[default]")
	maxKeyLen := 0
	for _, kd := range knownConfigKeys {
		if len(kd.key) > maxKeyLen {
			maxKeyLen = len(kd.key)
		}
	}
	for _, kd := range knownConfigKeys {
		var val string
		if v, ok := stored[kd.key]; ok {
			val = v
		} else if kd.defaultVal != "" {
			val = kd.defaultVal
		} else {
			val = "<unset>"
		}
		padding := strings.Repeat(" ", maxKeyLen-len(kd.key))
		fmt.Printf("  %s%s = %s\n", kd.key, padding, val)
	}

	// --- per-named-config sections ---
	names, err := wf.NamedConfigListAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config ls: %v\n", err)
		os.Exit(1)
	}
	for _, name := range names {
		fmt.Printf("\n[config: %s]\n", name)
		overrides, err := wf.NamedConfigList(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "config ls: %v\n", err)
			os.Exit(1)
		}
		if len(overrides) == 0 {
			fmt.Println("  (no overrides)")
			continue
		}
		maxOLen := 0
		for k := range overrides {
			if len(k) > maxOLen {
				maxOLen = len(k)
			}
		}
		// Emit in sorted order for stability.
		keys := make([]string, 0, len(overrides))
		for k := range overrides {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
			padding := strings.Repeat(" ", maxOLen-len(k))
			fmt.Printf("  %s%s = %s   (override)\n", k, padding, overrides[k])
		}
	}
}

func runConfigGet(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("config get", flag.ExitOnError)
	configName := fs.String("config", "", "named config to scope the lookup")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ticket config get [--config NAME] <key>")
		os.Exit(1)
	}
	key := fs.Arg(0)

	// Apply defaults for known keys.
	defaults := map[string]string{
		"workspace.type": "worktree",
	}

	if *configName != "" {
		value, err := wf.NamedConfigGetEffective(*configName, key, defaults[key])
		if err != nil {
			fmt.Fprintf(os.Stderr, "config get: %v\n", err)
			os.Exit(1)
		}
		if value == "" {
			fmt.Fprintf(os.Stderr, "config: key %q is not set\n", key)
			os.Exit(1)
		}
		fmt.Println(value)
		return
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

// sortStrings sorts a slice of strings in place (avoids import of sort in callers).
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
