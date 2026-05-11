package views

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/tui/components"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	comp "github.com/aidanwolter/ticket/internal/tui/components"
)

// ticketDetailStore is the minimal store surface needed by TicketDetailView.
type ticketDetailStore interface {
	GetTicket(id string) (*model.Ticket, error)
	GetThreadsForTask(taskID string) ([]*model.Thread, error)
	GetNotesForTicket(ticketID string) ([]*model.Note, error)
	GetAgentSessionByTicket(ticketID string) (*model.AgentSession, error)
}

type TicketDetailView struct {
	store        ticketDetailStore
	ticket       *model.Ticket
	notes        []*model.Note
	blockers     []*model.Ticket
	agentSession *model.AgentSession
	vp           viewport.Model
	width        int
	height       int
	err          error
}

func NewTicketDetailView(s ticketDetailStore, ticketID string) (*TicketDetailView, error) {
	v := &TicketDetailView{store: s}
	if err := v.loadTicket(ticketID); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *TicketDetailView) loadTicket(id string) error {
	t, err := v.store.GetTicket(id)
	if err != nil {
		return err
	}
	v.ticket = t

	// Populate per-task threads so the detail view can show counts.
	for i := range v.ticket.Tasks {
		threads, err := v.store.GetThreadsForTask(v.ticket.Tasks[i].ID)
		if err != nil {
			return err
		}
		v.ticket.Tasks[i].Threads = nil
		for _, th := range threads {
			v.ticket.Tasks[i].Threads = append(v.ticket.Tasks[i].Threads, *th)
		}
	}

	notes, err := v.store.GetNotesForTicket(id)
	if err != nil {
		return err
	}
	v.notes = notes

	v.blockers = nil
	for _, blockerID := range t.BlockedBy {
		blocker, err := v.store.GetTicket(blockerID)
		if err == nil {
			v.blockers = append(v.blockers, blocker)
		}
	}

	// Load active agent session (nil if none).
	v.agentSession, _ = v.store.GetAgentSessionByTicket(id)
	return nil
}

func (v *TicketDetailView) Reload() error {
	if v.ticket == nil {
		return nil
	}
	return v.loadTicket(v.ticket.ID)
}

func (v *TicketDetailView) Ticket() *model.Ticket {
	return v.ticket
}

func (v *TicketDetailView) SetSize(w, h int) {
	if v.width == w && v.height == h {
		return
	}
	v.width = w
	v.height = h
	v.vp = viewport.New(w, h-2)
	v.vp.SetContent(v.renderContent())
}

func (v *TicketDetailView) ScrollUp(n int)   { v.vp.LineUp(n) }
func (v *TicketDetailView) ScrollDown(n int) { v.vp.LineDown(n) }

func (v *TicketDetailView) Init() tea.Cmd { return nil }

func (v *TicketDetailView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		v.SetSize(ws.Width, ws.Height)
		return v, nil
	}
	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	return v, cmd
}

func (v *TicketDetailView) View() string {
	v.vp.SetContent(v.renderContent())
	hint := "[n] note · [[/]] scroll"
	if v.ticket != nil {
		switch v.ticket.Status {
		case model.StatusDraft:
			hint += " · [r] mark ready"
		case model.StatusReady:
			hint += " · [X] back to draft · [g] dispatch agent"
			if v.agentSession != nil {
				hint += " · [enter] attach"
			}
		case model.StatusInProgress:
			hint += " · [X] revert to draft"
		case model.StatusInReview:
			hint += " · [R] review · [X] revert to draft · [a] approve · [C] resolve conflicts"
		case model.StatusApproved:
			hint += " · [m] merge · [C] resolve conflicts"
		}
	}
	return v.vp.View() + "\n" +
		lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(hint)
}

func (v *TicketDetailView) renderContent() string {
	if v.ticket == nil {
		return "No ticket loaded."
	}
	t := v.ticket
	var sb strings.Builder

	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(
		fmt.Sprintf("%s  %s", t.ID, t.Title)) + "\n")

	sb.WriteString(fmt.Sprintf("  Status: %s  %s\n",
		components.TicketStatusIcon(t.Status)+" "+string(t.Status),
		lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("Updated: "+t.Updated.Format(time.RFC1123)),
	))

	if v.agentSession != nil {
		switch v.agentSession.State {
		case model.AgentRunning:
			sb.WriteString(fmt.Sprintf("  Agent: %s\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(
					fmt.Sprintf("running (pid %d)", v.agentSession.PID)),
			))
		case model.AgentWaiting:
			sb.WriteString(fmt.Sprintf("  Agent: %s\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(
					fmt.Sprintf("waiting for input (pid %d) — press enter to attach", v.agentSession.PID)),
			))
		}
	}

	if t.FeatureBranch != "" {
		sb.WriteString(fmt.Sprintf("  Branch: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render(t.FeatureBranch)))
	}
	if t.WorktreePath != "" {
		sb.WriteString(fmt.Sprintf("  Worktree: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render(t.WorktreePath)))
	}
	if t.RepoPath != "" {
		sb.WriteString(fmt.Sprintf("  Repo: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(t.RepoPath)))
	}
	sb.WriteString("\n")

	wrapWidth := v.width - 2
	if t.Description != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Description") + "\n")
		sb.WriteString(indent(wrapText(t.Description, wrapWidth), "  ") + "\n\n")
	}

	if len(v.blockers) > 0 {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Blocked By") + "\n")
		for _, b := range v.blockers {
			sb.WriteString(fmt.Sprintf("  %s %s %s\n",
				components.TicketStatusIcon(b.Status), b.ID, b.Title))
		}
		sb.WriteString("\n")
	}

	// Commits section (in_review only)
	if t.Status == model.StatusInReview && t.FeatureBranch != "" && t.RepoPath != "" {
		if commits := featureBranchCommits(t.RepoPath, t.FeatureBranch); len(commits) > 0 {
			sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
				fmt.Sprintf("Commits (%d)", len(commits))) + "\n")
			for _, c := range commits {
				sb.WriteString("  " + c + "\n")
			}
			sb.WriteString("\n")
		}
	}

	// Tasks section — grouped by round.
	if len(t.Tasks) == 0 {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Tasks") + "\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  no tasks") + "\n\n")
	} else {
		// Collect unique rounds in order.
		roundsSeen := make(map[int]bool)
		var rounds []int
		for _, task := range t.Tasks {
			r := task.Round
			if r == 0 {
				r = 1
			}
			if !roundsSeen[r] {
				roundsSeen[r] = true
				rounds = append(rounds, r)
			}
		}

		multiRound := len(rounds) > 1
		for _, round := range rounds {
			var roundTasks []model.Task
			for _, task := range t.Tasks {
				r := task.Round
				if r == 0 {
					r = 1
				}
				if r == round {
					roundTasks = append(roundTasks, task)
				}
			}

			rdone := 0
			for _, task := range roundTasks {
				if task.CompletedAt != nil {
					rdone++
				}
			}

			var header string
			if multiRound {
				label := fmt.Sprintf("Round %d", round)
				if round > 1 {
					label += " — amendments"
				}
				header = fmt.Sprintf("%s (%d/%d)", label, rdone, len(roundTasks))
			} else {
				header = fmt.Sprintf("Tasks (%d/%d)", rdone, len(roundTasks))
			}
			sb.WriteString(lipgloss.NewStyle().Bold(true).Render(header) + "\n")
			sb.WriteString("  " + comp.ProgressBar(rdone, len(roundTasks), 20) + "\n")

			for _, task := range roundTasks {
				icon := "○"
				col := lipgloss.Color("8")
				if task.CompletedAt != nil {
					icon = "●"
					col = lipgloss.Color("2")
				}
				taskLine := fmt.Sprintf("  %s %s. %s",
					lipgloss.NewStyle().Foreground(col).Render(icon),
					lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(fmt.Sprintf("%d", task.Position)),
					task.Title,
				)
				if task.CommitHash != "" {
					taskLine += " " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(task.CommitHash[:7])
				}
				if len(task.Threads) > 0 {
					open, needsAttention := 0, 0
					for _, th := range task.Threads {
						switch th.Status {
						case model.ThreadOpen:
							open++
						case model.ThreadNeedsAttention:
							needsAttention++
						}
					}
					var parts []string
					if open > 0 {
						parts = append(parts, fmt.Sprintf("%d open", open))
					}
					if needsAttention > 0 {
						parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(fmt.Sprintf("%d needs attention", needsAttention)))
					}
					if len(parts) > 0 {
						taskLine += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("("+strings.Join(parts, " · ")+")")
					}
				}
				sb.WriteString(taskLine + "\n")
				if task.Description != "" {
					sb.WriteString(indent(wrapText(task.Description, wrapWidth-4), "      ") + "\n")
				}
				if task.VerifiableResult != "" {
					sb.WriteString("      " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("✓ "+task.VerifiableResult) + "\n")
				}
			}
			sb.WriteString("\n")
		}
	}

	// Thread summary (aggregated across all tasks)
	open, needsAttention, resolved := 0, 0, 0
	for _, task := range t.Tasks {
		for _, th := range task.Threads {
			switch th.Status {
			case model.ThreadOpen:
				open++
			case model.ThreadNeedsAttention:
				needsAttention++
			case model.ThreadResolved:
				resolved++
			}
		}
	}
	total := open + needsAttention + resolved
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Threads (%d)", total)) + "\n")
	if total == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  no threads") + "\n")
	} else {
		sb.WriteString(fmt.Sprintf("  %s open  %s needs attention  %s resolved\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render(fmt.Sprintf("%d", open)),
			lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(fmt.Sprintf("%d", needsAttention)),
			lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(fmt.Sprintf("%d", resolved)),
		))
	}
	sb.WriteString("\n")

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Notes (%d)", len(v.notes))) + "\n")
	if len(v.notes) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  no notes") + "\n")
	} else {
		for _, n := range v.notes {
			authorStr := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(n.Author)
			noteWrap := v.width - 4 - len([]rune(n.Author))
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", authorStr, wrapText(n.Text, noteWrap)))
		}
	}

	return sb.String()
}

// featureBranchCommits returns one formatted line per commit on featureBranch
// that is not reachable from the default branch (HEAD of the repo). Returns nil
// on any error or if there are no such commits.
func featureBranchCommits(repoPath, featureBranch string) []string {
	// Determine the default branch name.
	headOut, err := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return nil
	}
	defaultBranch := strings.TrimSpace(string(headOut))

	ref := defaultBranch + ".." + featureBranch
	out, err := exec.Command("git", "-C", repoPath, "log", "--no-merges", "--format=%h\t%s\t%an", ref).Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	var lines []string
	for _, raw := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(raw, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		hash := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(parts[0])
		subject := parts[1]
		author := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(parts[2])
		lines = append(lines, fmt.Sprintf("%s  %s  %s", hash, subject, author))
	}
	return lines
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// wrapText hard-wraps s at width columns, preserving existing newlines.
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, wrapLine(line, width))
	}
	return strings.Join(out, "\n")
}

func wrapLine(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		if len([]rune(line))+1+len([]rune(word)) <= width {
			line += " " + word
		} else {
			lines = append(lines, line)
			line = word
		}
	}
	return strings.Join(append(lines, line), "\n")
}
