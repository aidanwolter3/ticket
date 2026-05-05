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

	// Auth redesign ticket with three implementation tasks.
	auth := &model.Ticket{
		Title:         "Auth redesign",
		Type:          model.TypeTicket,
		Status:        model.StatusReady,
		FeatureBranch: "feat/auth-argon2",
	}
	must(s.CreateTicket(auth))
	fmt.Println("Created", auth.ID)

	tasks := []struct {
		title, verifiable string
	}{
		{
			"Add JWT validation",
			"Run `npm test -- auth/jwt`. All tests pass.",
		},
		{
			"Replace bcrypt with argon2",
			"Run `npm test -- auth/`. All tests pass. Login works with both old and new hashes.",
		},
		{
			"Migrate user sessions",
			"Run `npm test -- auth/sessions`. All sessions migrate without forced logouts.",
		},
	}
	for i, td := range tasks {
		task := &model.Task{
			TicketID:         auth.ID,
			Title:            td.title,
			Position:         i + 1,
			VerifiableResult: td.verifiable,
		}
		must(s.CreateTask(task))
		fmt.Println("  Task", task.ID, task.Title)
	}

	// Standalone in_review ticket with a thread on its task.
	audit := &model.Ticket{
		Title:       "Audit log refactor",
		Type:        model.TypeTicket,
		Status:      model.StatusInReview,
		Description: "Clean up the audit log module to use structured logging.",
	}
	must(s.CreateTicket(audit))
	fmt.Println("Created", audit.ID, "(in_review)")

	auditTask := &model.Task{
		TicketID: audit.ID,
		Title:    "Refactor audit log to structured logging",
		Position: 1,
		VerifiableResult: "Run `go test ./pkg/logging/...`. All tests pass.",
	}
	must(s.CreateTask(auditTask))
	fmt.Println("  Task", auditTask.ID)

	thread, err := s.CreateThread(auditTask.ID)
	must(err)
	must2(s.AddMessage(thread.ID, "human:aidan", "The log rotation logic is duplicated in three places, please consolidate."))
	must2(s.AddMessage(thread.ID, "agent:claude", "I'll extract it into a shared helper in pkg/logging."))
	fmt.Println("  Added thread to task", auditTask.ID)

	fmt.Println("\nDone. Run: go run ./cmd/ticket/")
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func must2(_ interface{}, err error) {
	must(err)
}
