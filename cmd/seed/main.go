// Seed populates the DB with the example auth redesign scenario from the design doc.
// Usage: go run ./cmd/seed/ [--db path]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

func main() {
	home, _ := os.UserHomeDir()
	defaultDB := filepath.Join(home, ".local", "share", "ticket", "tickets.db")
	dbPath := flag.String("db", defaultDB, "path to SQLite database")
	flag.Parse()

	s, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	t043 := &model.Ticket{
		Title:            "Add JWT validation",
		Type:             model.TypeTicket,
		Status:           model.StatusReady,
		FeatureBranch:    "feat/auth-argon2",
		StackID:          "auth-1",
		VerifiableResult: "Run `npm test -- auth/jwt`. All tests pass.",
	}
	must(s.CreateTicket(t043))
	fmt.Println("Created", t043.ID)

	t044 := &model.Ticket{
		Title:            "Replace bcrypt with argon2",
		Type:             model.TypeTicket,
		Status:           model.StatusReady,
		FeatureBranch:    "feat/auth-argon2",
		StackID:          "auth-1",
		BlockedBy:        []string{t043.ID},
		VerifiableResult: "Run `npm test -- auth/`. All tests pass. Login works with both old and new hashes.",
	}
	must(s.CreateTicket(t044))
	fmt.Println("Created", t044.ID)

	t045 := &model.Ticket{
		Title:            "Migrate user sessions",
		Type:             model.TypeTicket,
		Status:           model.StatusReady,
		FeatureBranch:    "feat/auth-argon2",
		StackID:          "auth-1",
		BlockedBy:        []string{t044.ID},
		VerifiableResult: "Run `npm test -- auth/sessions`. All sessions migrate without forced logouts.",
	}
	must(s.CreateTicket(t045))
	fmt.Println("Created", t045.ID)

	t042 := &model.Ticket{
		Title:     "Auth redesign",
		Type:      model.TypePlan,
		Status:    model.StatusDraft,
		BlockedBy: []string{t043.ID, t044.ID, t045.ID},
	}
	must(s.CreateTicket(t042))
	fmt.Println("Created", t042.ID, "(plan)")

	t046 := &model.Ticket{
		Title:       "Audit log refactor",
		Type:        model.TypeTicket,
		Status:      model.StatusInReview,
		Description: "Clean up the audit log module to use structured logging.",
	}
	must(s.CreateTicket(t046))
	fmt.Println("Created", t046.ID, "(standalone in_review)")

	thread, _ := s.CreateThread(t046.ID)
	s.AddMessage(thread.ID, "human:aidan", "The log rotation logic is duplicated in three places, please consolidate.")
	s.AddMessage(thread.ID, "agent:claude", "I'll extract it into a shared helper in pkg/logging.")
	fmt.Println("Added thread to", t046.ID)

	fmt.Println("\nDone. Run: ./ticket")
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
