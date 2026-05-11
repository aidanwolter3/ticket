package views

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ReplyModal struct {
	authorIn textinput.Model
	textIn   textarea.Model
	focused  int
	threadID string
	width    int
}

func NewReplyModal(threadID string, width int) *ReplyModal {
	author := textinput.New()
	author.Placeholder = "Author (e.g., human:aidan)"
	author.SetValue("human:aidan")
	author.Focus()

	text := textarea.New()
	text.Placeholder = "Message"
	text.CharLimit = 500
	text.ShowLineNumbers = false
	text.SetWidth(max(width-4, 20))
	text.SetHeight(4)
	// Remove default cursor-line background which clashes with terminal colors.
	focusedStyle, blurredStyle := textarea.DefaultStyles()
	focusedStyle.CursorLine = lipgloss.NewStyle()
	text.FocusedStyle = focusedStyle
	text.BlurredStyle = blurredStyle

	return &ReplyModal{authorIn: author, textIn: text, threadID: threadID, width: width}
}

func (m *ReplyModal) SetWidth(w int) {
	m.width = w
	m.textIn.SetWidth(max(w-4, 20))
}

func (m *ReplyModal) Init() tea.Cmd { return textarea.Blink }

func (m *ReplyModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "tab" {
			if m.focused == 0 {
				m.authorIn.Blur()
				m.focused = 1
				return m, m.textIn.Focus()
			}
			m.textIn.Blur()
			m.focused = 0
			m.authorIn.Focus()
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

func (m *ReplyModal) Author() string   { return m.authorIn.Value() }
func (m *ReplyModal) Text() string     { return m.textIn.Value() }
func (m *ReplyModal) ThreadID() string { return m.threadID }

func (m *ReplyModal) View() string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Reply to Thread") + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Author") + "\n  " + m.authorIn.View() + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Message") + "\n" + m.textIn.View() + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"[tab] switch · [ctrl+s] send · [esc] cancel"))
	return sb.String()
}
