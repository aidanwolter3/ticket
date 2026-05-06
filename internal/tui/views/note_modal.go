package views

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type NoteModal struct {
	input    textinput.Model
	authorIn textinput.Model
	focused  int // 0=author, 1=text
}

func NewNoteModal() *NoteModal {
	author := textinput.New()
	author.Placeholder = "Author (e.g., human:aidan)"
	author.SetValue("human:aidan")
	author.Focus()

	text := textinput.New()
	text.Placeholder = "Note text"
	text.CharLimit = 500

	return &NoteModal{input: text, authorIn: author}
}

func (m *NoteModal) Init() tea.Cmd { return textinput.Blink }

func (m *NoteModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			if m.focused == 0 {
				m.authorIn.Blur()
				m.focused = 1
				m.input.Focus()
			} else {
				m.input.Blur()
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
		m.input, cmd = m.input.Update(msg)
	}
	return m, cmd
}

func (m *NoteModal) Author() string { return m.authorIn.Value() }
func (m *NoteModal) Text() string   { return m.input.Value() }
func (m *NoteModal) Focused() int   { return m.focused }

func (m *NoteModal) View() string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Add Note") + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Author") + "\n")
	sb.WriteString("  " + m.authorIn.View() + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Text") + "\n")
	sb.WriteString("  " + m.input.View() + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"[tab] switch · [enter/ctrl+s] save · [esc] cancel"))
	return sb.String()
}
