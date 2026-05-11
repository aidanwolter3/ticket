package views

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

// typeText sends individual character key events to a tea.Model.
func typeText(m tea.Model, text string) tea.Model {
	for _, r := range text {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		m, _ = m.Update(msg)
	}
	return m
}

func TestModal_EditDraft(t *testing.T) {
	t.Run("typed text is reflected in Text()", func(t *testing.T) {
		m := NewEditDraftModal("msg-1", "")
		m2 := typeText(m, "hello world")
		em := m2.(*EditDraftModal)
		assert.Contains(t, em.Text(), "hello world")
	})

	t.Run("initial text is pre-populated", func(t *testing.T) {
		m := NewEditDraftModal("msg-1", "existing text")
		assert.Equal(t, "existing text", m.Text())
	})

	t.Run("View contains title and hint", func(t *testing.T) {
		m := NewEditDraftModal("msg-1", "")
		out := stripANSI(m.View())
		assert.Contains(t, out, "Edit Draft Message")
		assert.Contains(t, out, "ctrl+s")
		assert.Contains(t, out, "esc")
	})

	t.Run("MsgID returns correct id", func(t *testing.T) {
		m := NewEditDraftModal("msg-42", "")
		assert.Equal(t, "msg-42", m.MsgID())
	})
}

func TestModal_NewThread(t *testing.T) {
	t.Run("typed text in textarea is reflected in Text()", func(t *testing.T) {
		m := NewNewThreadModal("task-1", "", "", 80)
		// Tab to focus the textarea.
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tab"), Alt: false})
		nm := m2.(*NewThreadModal)
		nm2 := typeText(nm, "thread message")
		nm3 := nm2.(*NewThreadModal)
		assert.Contains(t, nm3.Text(), "thread message")
	})

	t.Run("default author is pre-populated", func(t *testing.T) {
		m := NewNewThreadModal("task-1", "", "", 80)
		assert.Equal(t, "human:aidan", m.Author())
	})

	t.Run("View contains expected labels", func(t *testing.T) {
		m := NewNewThreadModal("task-1", "file.go", "@@ -1,5 +1,7 @@", 80)
		out := stripANSI(m.View())
		assert.Contains(t, out, "New Thread")
		assert.Contains(t, out, "Author")
		assert.Contains(t, out, "Message")
	})

	t.Run("accessors return constructor values", func(t *testing.T) {
		m := NewNewThreadModal("task-99", "main.go", "@@ -5,3 +5,4 @@", 80)
		assert.Equal(t, "task-99", m.TaskID())
		assert.Equal(t, "main.go", m.FilePath())
		assert.Equal(t, "@@ -5,3 +5,4 @@", m.HunkHeader())
	})
}

func TestModal_Reply(t *testing.T) {
	t.Run("typed text in textarea is reflected in Text()", func(t *testing.T) {
		m := NewReplyModal("thread-1", 80)
		// Tab to focus textarea.
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tab")})
		rm := m2.(*ReplyModal)
		rm2 := typeText(rm, "reply message")
		rm3 := rm2.(*ReplyModal)
		assert.Contains(t, rm3.Text(), "reply message")
	})

	t.Run("default author is pre-populated", func(t *testing.T) {
		m := NewReplyModal("thread-1", 80)
		assert.Equal(t, "human:aidan", m.Author())
	})

	t.Run("ThreadID returns constructor value", func(t *testing.T) {
		m := NewReplyModal("thread-99", 80)
		assert.Equal(t, "thread-99", m.ThreadID())
	})

	t.Run("View contains expected labels", func(t *testing.T) {
		m := NewReplyModal("thread-1", 80)
		out := stripANSI(m.View())
		assert.Contains(t, out, "Reply to Thread")
		assert.Contains(t, out, "Author")
		assert.Contains(t, out, "ctrl+s")
		assert.Contains(t, out, "esc")
	})
}
