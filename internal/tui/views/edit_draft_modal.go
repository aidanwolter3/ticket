package views

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type EditDraftModal struct {
	textIn textinput.Model
	msgID  string
}

func NewEditDraftModal(msgID, currentText string) *EditDraftModal {
	t := textinput.New()
	t.Placeholder = "Message"
	t.CharLimit = 500
	t.SetValue(currentText)
	t.CursorEnd()
	t.Focus()
	return &EditDraftModal{textIn: t, msgID: msgID}
}

func (m *EditDraftModal) MsgID() string { return m.msgID }
func (m *EditDraftModal) Text() string  { return m.textIn.Value() }

func (m *EditDraftModal) Init() tea.Cmd { return textinput.Blink }

func (m *EditDraftModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.textIn, cmd = m.textIn.Update(msg)
	return m, cmd
}

func (m *EditDraftModal) View() string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Edit Draft Message") + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Message") + "\n  " + m.textIn.View() + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"[ctrl+s] save · [esc] cancel"))
	return sb.String()
}
