package cli

import (
	"flag"
	"fmt"
	"os"
)

// RunPurge deletes the database file at dbPath after confirmation.
// It takes the resolved db path directly because it doesn't need an open store.
func RunPurge(args []string, dbPath string) {
	fs := flag.NewFlagSet("purge", flag.ExitOnError)
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	fs.Parse(args)

	if !*yes {
		fmt.Fprintf(os.Stderr, "purge %s? This cannot be undone. Pass --yes to confirm.\n", dbPath)
		os.Exit(1)
	}

	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "purge: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("purged %s\n", dbPath)
}
