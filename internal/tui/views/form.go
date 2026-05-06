package views

import (
	"fmt"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type FormField int

const (
	FieldTitle FormField = iota
	FieldDescription
	FieldBranch
	FieldBlockedBy
	FieldStatus
	fieldCount
)

type FormView struct {
	fields   [fieldCount]textinput.Model
	focused  FormField
	isEdit   bool
	ticketID string
	err      string
	width    int
	height   int
}

func NewFormView(existing *model.Ticket) *FormView {
	f := &FormView{}
	for i := range f.fields {
		ti := textinput.New()
		ti.CharLimit = 200
		f.fields[i] = ti
	}
	f.fields[FieldTitle].Placeholder = "Title (required)"
	f.fields[FieldDescription].Placeholder = "Description"
	f.fields[FieldBranch].Placeholder = "Feature branch"
	f.fields[FieldBlockedBy].Placeholder = "Blocked by (comma-separated IDs)"
	f.fields[FieldStatus].Placeholder = "Status"

	if existing != nil {
		f.isEdit = true
		f.ticketID = existing.ID
		f.fields[FieldTitle].SetValue(existing.Title)
		f.fields[FieldDescription].SetValue(existing.Description)
		f.fields[FieldBranch].SetValue(existing.FeatureBranch)
		f.fields[FieldBlockedBy].SetValue(strings.Join(existing.BlockedBy, ", "))
		f.fields[FieldStatus].SetValue(string(existing.Status))
	} else {
		f.fields[FieldStatus].SetValue("draft")
	}
	f.fields[FieldTitle].Focus()
	return f
}

func (f *FormView) SetSize(w, h int) { f.width = w; f.height = h }
func (f *FormView) Init() tea.Cmd    { return textinput.Blink }

func (f *FormView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			f.fields[f.focused].Blur()
			f.focused = (f.focused + 1) % fieldCount
			f.fields[f.focused].Focus()
			return f, nil
		case "shift+tab":
			f.fields[f.focused].Blur()
			if f.focused == 0 {
				f.focused = fieldCount - 1
			} else {
				f.focused--
			}
			f.fields[f.focused].Focus()
			return f, nil
		}
	case tea.WindowSizeMsg:
		f.width = msg.Width
		f.height = msg.Height
	}
	var cmd tea.Cmd
	f.fields[f.focused], cmd = f.fields[f.focused].Update(msg)
	return f, cmd
}

func (f *FormView) Validate() error {
	if strings.TrimSpace(f.fields[FieldTitle].Value()) == "" {
		return fmt.Errorf("title is required")
	}
	return nil
}

func (f *FormView) ToTicket() *model.Ticket {
	blockedBy := parseIDs(f.fields[FieldBlockedBy].Value())
	status := model.Status(f.fields[FieldStatus].Value())
	switch status {
	case model.StatusDraft, model.StatusReady, model.StatusInProgress, model.StatusInReview, model.StatusCompleted:
	default:
		status = model.StatusDraft
	}
	t := &model.Ticket{
		Title:         strings.TrimSpace(f.fields[FieldTitle].Value()),
		Type:          model.TypeTicket,
		Description:   f.fields[FieldDescription].Value(),
		FeatureBranch: f.fields[FieldBranch].Value(),
		BlockedBy:     blockedBy,
		Status:        status,
	}
	if f.isEdit {
		t.ID = f.ticketID
	}
	return t
}

func (f *FormView) IsEdit() bool     { return f.isEdit }
func (f *FormView) TicketID() string { return f.ticketID }

func (f *FormView) View() string {
	var sb strings.Builder

	title := "New Ticket"
	if f.isEdit {
		title = "Edit Ticket " + f.ticketID
	}
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title) + "\n\n")

	labels := []string{"Title", "Description", "Branch", "Blocked By", "Status"}
	for i, label := range labels {
		prefix := "  "
		if FormField(i) == f.focused {
			prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render("> ")
		}
		sb.WriteString(prefix + lipgloss.NewStyle().Bold(true).Render(label) + "\n")
		sb.WriteString("  " + f.fields[i].View() + "\n\n")
	}

	if f.err != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Error: "+f.err) + "\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		"[tab] next · [shift+tab] prev · [ctrl+s] save · [esc] cancel"))

	return sb.String()
}

func parseIDs(s string) []string {
	var ids []string
	for _, part := range strings.Split(s, ",") {
		id := strings.TrimSpace(part)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
