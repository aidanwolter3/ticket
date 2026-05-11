package tui

import (
	"os"
	"testing"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/workflow/human"
)

// newTestApp opens a temp DB, creates tickets with the given IDs (in order),
// optionally marks some as waiting, and returns a ready-to-test App.
func newTestApp(t *testing.T, ticketIDs []string, waitingIDs []string) *App {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()

	s, err := store.Open(f.Name())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	waiting := make(map[string]bool, len(waitingIDs))
	for _, id := range waitingIDs {
		waiting[id] = true
	}

	for _, id := range ticketIDs {
		ticket := &model.Ticket{
			ID:     id,
			Title:  "ticket " + id,
			Status: model.StatusDraft,
			Type:   model.TypeTicket,
		}
		// Insert directly with the fixed ID by using store internals via SQL.
		// We bypass nextTicketID by using a raw exec through the exported CreateTicket path.
		// CreateTicket auto-assigns an ID, so we set the ID field and rely on the insert.
		// NOTE: CreateTicket assigns its own ID — we'll create then update the ID.
		if err := s.CreateTicket(ticket); err != nil {
			t.Fatalf("create ticket %s: %v", id, err)
		}
		// If this ticket should have a waiting agent session, create one.
		if waiting[ticket.ID] {
			sess, err := s.CreateAgentSession(ticket.ID, 0, "/dev/null")
			if err != nil {
				t.Fatalf("create session for %s: %v", ticket.ID, err)
			}
			if err := s.UpdateAgentSessionState(sess.ID, model.AgentWaiting); err != nil {
				t.Fatalf("update session state: %v", err)
			}
		}
	}

	return New(human.New(s))
}

func TestNextWaitingTicketID_NoWaiting(t *testing.T) {
	app := newTestApp(t, []string{"ticket1", "ticket2"}, nil)
	if got := app.nextWaitingTicketID(); got != "" {
		t.Errorf("expected empty string when no agents waiting, got %q", got)
	}
}

func TestNextWaitingTicketID_SingleWaiting(t *testing.T) {
	// Helper to get actual IDs from the store after creation.
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()
	s, err := store.Open(f.Name())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	t1 := &model.Ticket{Title: "A", Status: model.StatusDraft, Type: model.TypeTicket}
	t2 := &model.Ticket{Title: "B", Status: model.StatusDraft, Type: model.TypeTicket}
	t3 := &model.Ticket{Title: "C", Status: model.StatusDraft, Type: model.TypeTicket}
	for _, tk := range []*model.Ticket{t1, t2, t3} {
		if err := s.CreateTicket(tk); err != nil {
			t.Fatalf("create ticket: %v", err)
		}
	}
	// Mark t2 as waiting.
	sess, err := s.CreateAgentSession(t2.ID, 0, "/dev/null")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.UpdateAgentSessionState(sess.ID, model.AgentWaiting); err != nil {
		t.Fatalf("update state: %v", err)
	}

	app := New(human.New(s))

	// Regardless of which ticket is focused, should return t2.
	got := app.nextWaitingTicketID()
	if got != t2.ID {
		t.Errorf("expected %s, got %q", t2.ID, got)
	}
}

func TestNextWaitingTicketID_CyclesInOrder(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()
	s, err := store.Open(f.Name())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	t1 := &model.Ticket{Title: "A", Status: model.StatusDraft, Type: model.TypeTicket}
	t2 := &model.Ticket{Title: "B", Status: model.StatusDraft, Type: model.TypeTicket}
	t3 := &model.Ticket{Title: "C", Status: model.StatusDraft, Type: model.TypeTicket}
	t4 := &model.Ticket{Title: "D", Status: model.StatusDraft, Type: model.TypeTicket}
	for _, tk := range []*model.Ticket{t1, t2, t3, t4} {
		if err := s.CreateTicket(tk); err != nil {
			t.Fatalf("create ticket: %v", err)
		}
	}
	// Mark t1 and t3 as waiting.
	for _, tk := range []*model.Ticket{t1, t3} {
		sess, err := s.CreateAgentSession(tk.ID, 0, "/dev/null")
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		if err := s.UpdateAgentSessionState(sess.ID, model.AgentWaiting); err != nil {
			t.Fatalf("update state: %v", err)
		}
	}

	app := New(human.New(s))

	// Focus on t1 → next waiting should be t3.
	app.ticketsView.SelectTicketByID(t1.ID)
	if got := app.nextWaitingTicketID(); got != t3.ID {
		t.Errorf("from t1: expected %s, got %q", t3.ID, got)
	}

	// Focus on t3 → next waiting should wrap around to t1.
	app.ticketsView.SelectTicketByID(t3.ID)
	if got := app.nextWaitingTicketID(); got != t1.ID {
		t.Errorf("from t3: expected %s (wrap), got %q", t1.ID, got)
	}

	// Focus on t2 (not waiting) → next waiting should be t3.
	app.ticketsView.SelectTicketByID(t2.ID)
	if got := app.nextWaitingTicketID(); got != t3.ID {
		t.Errorf("from t2: expected %s, got %q", t3.ID, got)
	}
}

func TestNextWaitingTicketID_WrapAround(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()
	s, err := store.Open(f.Name())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	t1 := &model.Ticket{Title: "A", Status: model.StatusDraft, Type: model.TypeTicket}
	t2 := &model.Ticket{Title: "B", Status: model.StatusDraft, Type: model.TypeTicket}
	t3 := &model.Ticket{Title: "C", Status: model.StatusDraft, Type: model.TypeTicket}
	for _, tk := range []*model.Ticket{t1, t2, t3} {
		if err := s.CreateTicket(tk); err != nil {
			t.Fatalf("create ticket: %v", err)
		}
	}
	// Only t1 is waiting.
	sess, err := s.CreateAgentSession(t1.ID, 0, "/dev/null")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.UpdateAgentSessionState(sess.ID, model.AgentWaiting); err != nil {
		t.Fatalf("update state: %v", err)
	}

	app := New(human.New(s))

	// Focus on t3 (last) → must wrap around to t1.
	app.ticketsView.SelectTicketByID(t3.ID)
	if got := app.nextWaitingTicketID(); got != t1.ID {
		t.Errorf("wrap-around: expected %s, got %q", t1.ID, got)
	}
}
