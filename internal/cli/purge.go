package cli

import (
	"flag"
	"fmt"
	"os"
)

func RunPurge(args []string, defaultDB string) {
	fs := flag.NewFlagSet("purge", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	fs.Parse(args)

	if !*yes {
		fmt.Fprintf(os.Stderr, "purge %s? This cannot be undone. Pass --yes to confirm.\n", *dbPath)
		os.Exit(1)
	}

	if err := os.Remove(*dbPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "purge: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("purged %s\n", *dbPath)
}
