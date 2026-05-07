package cli

import (
	"flag"

	"github.com/aidanwolter/ticket/internal/store"
)

// parseAndOpen registers the shared --db flag, calls setup for any
// caller-specific flags, parses args, opens the store, and returns both.
// Caller must defer s.Close().
func parseAndOpen(name string, args []string, defaultDB string, setup func(*flag.FlagSet)) (*store.Store, *flag.FlagSet) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	if setup != nil {
		setup(fs)
	}
	fs.Parse(args)
	s := openStore(*dbPath)
	return s, fs
}
