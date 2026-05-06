package cli

import (
	"fmt"
	"os"

	"github.com/aidanwolter/ticket/internal/store"
)

func openStore(dbPath string) *store.Store {
	s, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open db: %v\n", err)
		os.Exit(1)
	}
	return s
}
