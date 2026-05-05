package views

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type NewThreadModal struct {
	authorIn textinput.Model
	textIn   textinput.Model
	focused  int
	taskID   string
}

func NewNewThreadModal(taskID string) *NewThreadModal {
	author := textinput.New()
	author.Placeholder = "Author (e.g., human:aidan)"
	author.SetValue("human:aidan")
	author.Focus()

	text := textinput.New()
	text.Placeholder = "First message"
	text.CharLimit = 500

	return &NewThreadModal{authorIn: author, textIn: text, taskID: taskID}
}

func (m *NewThreadModal) Init() tea.Cmd { return textinput.Blink }

func (m *NewThreadModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "tab" {
			if m.focused == 0 {
				m.authorIn.Blur()
				m.focused = 1
				m.textIn.Focus()
			} else {
				m.textIn.Blur()
				m.focused = 0
				m.authorIn.Focus()
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	if m.focused == 0 {
		m.authorIn, cmd = m.authorIn.Update(msg)
	} else {
		m.textIn, cmd = m.textIn.Update(msg)
	}
	return m, cmd
}

func (m *NewThreadModal) Author() string { return m.authorIn.Value() }
func (m *NewThreadModal) Text() string   { return m.textIn.Value() }
func (m *NewThreadModal) TaskID() string { return m.taskID }

func (m *NewThreadModal) View() string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("New Thread") + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Author") + "\n  " + m.authorIn.View() + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Message") + "\n  " + m.textIn.View() + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"tab switch · ctrl+s create · esc cancel"))
	return sb.String()
}
