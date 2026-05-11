package views

import (
	"strings"
	"testing"

	"github.com/aidanwolter/ticket/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sendTicketsKey(v *TicketsView, key string) *TicketsView {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	m, _ := v.Update(msg)
	return m.(*TicketsView)
}

func TestTicketsView(t *testing.T) {
	t.Run("visible filtering hides merged by default", func(t *testing.T) {
		s := newTestStore(t)
		draft := &model.Ticket{Title: "Draft", Type: model.TypeTicket, Status: model.StatusDraft}
		merged := &model.Ticket{Title: "Merged", Type: model.TypeTicket, Status: model.StatusMerged}
		require.NoError(t, s.CreateTicket(draft))
		require.NoError(t, s.CreateTicket(merged))

		v := NewTicketsView(s)
		vis := v.visible()
		ids := make([]string, len(vis))
		for i, t := range vis {
			ids[i] = t.ID
		}
		assert.Contains(t, ids, draft.ID)
		assert.NotContains(t, ids, merged.ID)
	})

	t.Run("toggle hideMerged shows merged tickets", func(t *testing.T) {
		s := newTestStore(t)
		merged := &model.Ticket{Title: "Merged", Type: model.TypeTicket, Status: model.StatusMerged}
		require.NoError(t, s.CreateTicket(merged))

		v := NewTicketsView(s)
		// 'h' toggles hideMerged.
		v = sendTicketsKey(v, "h")

		vis := v.visible()
		ids := make([]string, len(vis))
		for i, t := range vis {
			ids[i] = t.ID
		}
		assert.Contains(t, ids, merged.ID)
	})

	t.Run("cursor down stops at last item", func(t *testing.T) {
		s := newTestStore(t)
		for i := 1; i <= 3; i++ {
			tkt := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
			require.NoError(t, s.CreateTicket(tkt))
		}
		v := NewTicketsView(s)
		v.SetSize(120, 40)

		for i := 0; i < 10; i++ {
			v = sendTicketsKey(v, "j")
		}
		assert.Equal(t, 2, v.cursor)
	})

	t.Run("cursor up stops at 0", func(t *testing.T) {
		s := newTestStore(t)
		for i := 1; i <= 3; i++ {
			tkt := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
			require.NoError(t, s.CreateTicket(tkt))
		}
		v := NewTicketsView(s)
		v.SetSize(120, 40)

		// Move to end then navigate up past 0.
		v = sendTicketsKey(v, "j")
		v = sendTicketsKey(v, "j")
		for i := 0; i < 10; i++ {
			v = sendTicketsKey(v, "k")
		}
		assert.Equal(t, 0, v.cursor)
	})

	t.Run("agent indicator appears in View for running session", func(t *testing.T) {
		s := newTestStore(t)
		tkt := &model.Ticket{Title: "AgentTicket", Type: model.TypeTicket, Status: model.StatusInProgress}
		require.NoError(t, s.CreateTicket(tkt))
		_, err := s.CreateAgentSession(tkt.ID, 1234, "/tmp/log.txt")
		require.NoError(t, err)

		v := NewTicketsView(s)
		v.SetSize(120, 40)

		out := v.View()
		assert.Contains(t, out, "⚙")
	})

	t.Run("empty store renders without panic and shows no ticket IDs", func(t *testing.T) {
		s := newTestStore(t)
		v := NewTicketsView(s)
		v.SetSize(120, 40)

		out := v.View()
		assert.NotPanics(t, func() { _ = v.View() })
		assert.False(t, strings.Contains(out, "T-001"))
	})

	t.Run("load after store change picks up new ticket", func(t *testing.T) {
		s := newTestStore(t)
		v := NewTicketsView(s)

		tkt := &model.Ticket{Title: "New Ticket", Type: model.TypeTicket, Status: model.StatusDraft}
		require.NoError(t, s.CreateTicket(tkt))
		v.load()

		ids := make([]string, len(v.visible()))
		for i, t := range v.visible() {
			ids[i] = t.ID
		}
		assert.Contains(t, ids, tkt.ID)
	})

	t.Run("j moves cursor down", func(t *testing.T) {
		s := newTestStore(t)
		for i := 1; i <= 3; i++ {
			tkt := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
			require.NoError(t, s.CreateTicket(tkt))
		}
		v := NewTicketsView(s)
		v.SetSize(120, 40)

		assert.Equal(t, 0, v.cursor)
		v = sendTicketsKey(v, "j")
		assert.Equal(t, 1, v.cursor)
	})

	t.Run("k moves cursor up", func(t *testing.T) {
		s := newTestStore(t)
		for i := 1; i <= 3; i++ {
			tkt := &model.Ticket{Title: "T", Type: model.TypeTicket, Status: model.StatusDraft}
			require.NoError(t, s.CreateTicket(tkt))
		}
		v := NewTicketsView(s)
		v.SetSize(120, 40)

		v = sendTicketsKey(v, "j")
		v = sendTicketsKey(v, "j")
		assert.Equal(t, 2, v.cursor)
		v = sendTicketsKey(v, "k")
		assert.Equal(t, 1, v.cursor)
	})

	t.Run("SelectedTicket returns focused ticket", func(t *testing.T) {
		s := newTestStore(t)
		t1 := &model.Ticket{Title: "First", Type: model.TypeTicket, Status: model.StatusDraft}
		t2 := &model.Ticket{Title: "Second", Type: model.TypeTicket, Status: model.StatusDraft}
		require.NoError(t, s.CreateTicket(t1))
		require.NoError(t, s.CreateTicket(t2))

		v := NewTicketsView(s)
		v.SetSize(120, 40)

		assert.Equal(t, t1.ID, v.SelectedTicket().ID)
		v = sendTicketsKey(v, "j")
		assert.Equal(t, t2.ID, v.SelectedTicket().ID)
	})
}
