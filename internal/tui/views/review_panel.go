package views

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
	"github.com/aidanwolter/ticket/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// renderedLine is a line in the right pane with hunk context for [c] anchoring.
type renderedLine struct {
	text     string // pre-styled fixed content (header, annotations); rendered as-is
	rawText  string // tab-expanded unstyled diff text; horizontal-scrolled at render time
	lineKind string // styling to apply to rawText: "add", "del", "hunk", "bold", ""
	filePath string
	hunkHdr  string
	threadID string // non-empty for thread annotation lines
	isHunk   bool   // true when this line is itself a @@ hunk header
}

// ReviewPanelView is the full-screen code-review split-pane overlay.
type ReviewPanelView struct {
	store          *store.Store
	ticket         *model.Ticket
	tasks          []model.Task
	taskCursor     int
	taskListOffset int
	lines          []renderedLine
	offset         int
	hOffset        int // horizontal scroll offset for diff lines
	width          int
	height         int
	threads        []*model.Thread
	draftState     *model.DraftState
}

func NewReviewPanelView(s *store.Store, ticketID string) (*ReviewPanelView, error) {
	v := &ReviewPanelView{store: s}
	return v, v.load(ticketID)
}

func (v *ReviewPanelView) load(ticketID string) error {
	ticket, err := v.store.GetTicket(ticketID)
	if err != nil {
		return err
	}
	v.ticket = ticket

	tasks, err := v.store.GetTasksForTicket(ticketID)
	if err != nil {
		return err
	}
	v.tasks = tasks

	threads, err := v.store.GetAllThreadsForTicket(ticketID)
	if err != nil {
		return err
	}
	v.threads = threads

	ds, err := v.store.GetDraftState(ticketID)
	if err != nil {
		return err
	}
	v.draftState = ds

	v.buildDiffLines()
	return nil
}

func (v *ReviewPanelView) Reload() error {
	if v.ticket == nil {
		return nil
	}
	id := v.ticket.ID
	return v.load(id)
}

func (v *ReviewPanelView) TicketID() string {
	if v.ticket == nil {
		return ""
	}
	return v.ticket.ID
}

func (v *ReviewPanelView) SelectedTaskID() string {
	if v.taskCursor < len(v.tasks) {
		return v.tasks[v.taskCursor].ID
	}
	return ""
}

// HunkContext returns the file_path and hunk_header for the hunk at or above the
// current scroll offset in the right pane.
func (v *ReviewPanelView) HunkContext() (filePath, hunkHeader string) {
	idx := v.offset
	if idx < 0 {
		idx = 0
	}
	for i := idx; i >= 0; i-- {
		if i < len(v.lines) {
			l := v.lines[i]
			if l.filePath != "" && l.hunkHdr != "" {
				return l.filePath, l.hunkHdr
			}
		}
	}
	// Fall back to whatever the offset line has (may be empty).
	if idx < len(v.lines) {
		return v.lines[idx].filePath, v.lines[idx].hunkHdr
	}
	return "", ""
}

func (v *ReviewPanelView) buildDiffLines() {
	v.lines = nil
	v.offset = 0
	v.hOffset = 0
	if v.taskCursor >= len(v.tasks) {
		return
	}
	task := v.tasks[v.taskCursor]
	rw := v.rightW()

	// Task header: title (h-scrollable) + description (word-wrapped, fixed) above the diff.
	v.lines = append(v.lines, renderedLine{rawText: fmt.Sprintf("Task %d: %s", task.Position, task.Title), lineKind: "bold"})
	v.lines = append(v.lines, renderedLine{text: ""})
	for _, l := range strings.Split(wrapText(task.Description, rw-2), "\n") {
		v.lines = append(v.lines, renderedLine{text: l})
	}
	v.lines = append(v.lines, renderedLine{text: ""})
	v.lines = append(v.lines, renderedLine{text: strings.Repeat("─", rw)})
	v.lines = append(v.lines, renderedLine{text: ""})

	if task.CommitHash == "" {
		v.lines = append(v.lines, renderedLine{
			text: lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("(task does not have a commit)"),
		})
		return
	}

	repoPath := ""
	if v.ticket != nil {
		repoPath = v.ticket.RepoPath
	}
	var rawLines []string
	if repoPath != "" {
		cmd := exec.Command("git", "show", task.CommitHash)
		cmd.Dir = repoPath
		out, _ := cmd.Output()
		rawLines = strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	} else {
		rawLines = []string{"(no repo_path configured for this ticket)"}
	}

	v.buildAnnotatedLines(rawLines)
}

type hunkThreads struct {
	real  []*model.Thread
	draft []*model.DraftThread
}

func (v *ReviewPanelView) threadsByHunk() map[string]*hunkThreads {
	m := make(map[string]*hunkThreads)
	key := func(fp, hh string) string { return fp + "\x00" + hh }
	ensure := func(fp, hh string) *hunkThreads {
		k := key(fp, hh)
		if m[k] == nil {
			m[k] = &hunkThreads{}
		}
		return m[k]
	}
	for _, th := range v.threads {
		if th.FilePath != "" && th.HunkHeader != "" {
			ht := ensure(th.FilePath, th.HunkHeader)
			ht.real = append(ht.real, th)
		}
	}
	if v.draftState != nil {
		for i := range v.draftState.NewThreads {
			dt := &v.draftState.NewThreads[i]
			if dt.FilePath != "" && dt.HunkHeader != "" {
				ht := ensure(dt.FilePath, dt.HunkHeader)
				ht.draft = append(ht.draft, dt)
			}
		}
	}
	return m
}

// applyLineKind applies syntax highlighting to a visible diff line slice.
func applyLineKind(kind, text string) string {
	switch kind {
	case "add":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(text)
	case "del":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(text)
	case "hunk":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(text)
	case "bold":
		return lipgloss.NewStyle().Bold(true).Render(text)
	default:
		return text
	}
}

func (v *ReviewPanelView) buildAnnotatedLines(rawLines []string) {
	// Note: v.lines already contains the task header prepended by buildDiffLines — append here.
	htMap := v.threadsByHunk()

	currentFile := ""
	currentHunk := ""

	flushHunkAnnotations := func(fp, hh string) {
		k := fp + "\x00" + hh
		ht := htMap[k]
		if ht == nil {
			return
		}
		for _, th := range ht.real {
			summary := th.Summary()
			if len([]rune(summary)) > 60 {
				summary = string([]rune(summary)[:60]) + "…"
			}
			msgCount := fmt.Sprintf("(%d msg)", len(th.Messages))
			icon := components.ThreadStatusIcon(th.Status)
			line := fmt.Sprintf("    ┆ %s %s %s", icon, summary,
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(msgCount))
			v.lines = append(v.lines, renderedLine{text: line, filePath: fp, hunkHdr: hh, threadID: th.ID})
		}
		for _, dt := range ht.draft {
			summary := "(empty draft)"
			if len(dt.Messages) > 0 {
				s := dt.Messages[0].Text
				if len([]rune(s)) > 60 {
					s = string([]rune(s)[:60]) + "…"
				}
				summary = s
			}
			icon := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("◌")
			line := fmt.Sprintf("    ┆ %s %s %s", icon, summary,
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[draft]"))
			v.lines = append(v.lines, renderedLine{text: line, filePath: fp, hunkHdr: hh})
		}
	}

	for _, raw := range rawLines {
		isHunk := false
		if strings.HasPrefix(raw, "diff --git ") {
			// Flush annotations for previous hunk before starting a new file.
			flushHunkAnnotations(currentFile, currentHunk)
			parts := strings.Fields(raw)
			if len(parts) >= 4 {
				b := parts[3]
				if strings.HasPrefix(b, "b/") {
					currentFile = b[2:]
				}
			}
			currentHunk = ""
		} else if strings.HasPrefix(raw, "@@ ") {
			// Flush annotations for previous hunk before starting a new hunk.
			flushHunkAnnotations(currentFile, currentHunk)
			currentHunk = raw
			isHunk = true
		}

		// Determine line kind for syntax highlighting.
		var lk string
		switch {
		case strings.HasPrefix(raw, "+++") || strings.HasPrefix(raw, "---"):
			lk = "bold"
		case strings.HasPrefix(raw, "+"):
			lk = "add"
		case strings.HasPrefix(raw, "-"):
			lk = "del"
		case strings.HasPrefix(raw, "@@"):
			lk = "hunk"
		case strings.HasPrefix(raw, "diff --git") ||
			strings.HasPrefix(raw, "index ") ||
			strings.HasPrefix(raw, "new file") ||
			strings.HasPrefix(raw, "deleted file"):
			lk = "bold"
		}

		// Expand tabs to spaces so rune width matches visual width.
		expanded := strings.ReplaceAll(raw, "\t", "    ")

		v.lines = append(v.lines, renderedLine{rawText: expanded, lineKind: lk, filePath: currentFile, hunkHdr: currentHunk, isHunk: isHunk})
	}
	// Flush annotations after the last hunk.
	flushHunkAnnotations(currentFile, currentHunk)
}

func (v *ReviewPanelView) rightW() int {
	leftW := v.width * 35 / 100
	if leftW < 20 {
		leftW = 20
	}
	rw := v.width - leftW - 1
	if rw < 10 {
		rw = 10
	}
	return rw
}

func (v *ReviewPanelView) leftW() int {
	lw := v.width * 35 / 100
	if lw < 20 {
		lw = 20
	}
	return lw
}

func (v *ReviewPanelView) bodyH() int {
	// Subtract 4: tab bar + divider + hint line (rendered by this view) + status bar (rendered by App).
	h := v.height - 4
	if h < 1 {
		h = 1
	}
	return h
}

func (v *ReviewPanelView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.buildDiffLines()
}

func (v *ReviewPanelView) Init() tea.Cmd { return nil }

func (v *ReviewPanelView) clampOffset() {
	bh := v.bodyH()
	maxOffset := len(v.lines) - bh
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.offset > maxOffset {
		v.offset = maxOffset
	}
	if v.offset < 0 {
		v.offset = 0
	}
}

func (v *ReviewPanelView) clampHOffset() {
	if v.hOffset < 0 {
		v.hOffset = 0
	}
}

func (v *ReviewPanelView) updateTaskListOffset() {
	bh := v.bodyH()
	if v.taskCursor < v.taskListOffset {
		v.taskListOffset = v.taskCursor
	}
	if v.taskCursor >= v.taskListOffset+bh {
		v.taskListOffset = v.taskCursor - bh + 1
	}
	if v.taskListOffset < 0 {
		v.taskListOffset = 0
	}
}

func (v *ReviewPanelView) jumpToNextHunk() {
	for i := v.offset + 1; i < len(v.lines); i++ {
		if v.lines[i].isHunk {
			v.offset = i
			v.clampOffset()
			return
		}
	}
}

func (v *ReviewPanelView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "up", "k":
			if v.taskCursor > 0 {
				v.taskCursor--
				v.buildDiffLines()
				v.updateTaskListOffset()
			}
		case "down", "j":
			if v.taskCursor < len(v.tasks)-1 {
				v.taskCursor++
				v.buildDiffLines()
				v.updateTaskListOffset()
			}
		case "[":
			v.offset--
			v.clampOffset()
		case "]":
			v.offset++
			v.clampOffset()
		case "n":
			v.jumpToNextHunk()
		case "<":
			v.hOffset -= 4
			v.clampHOffset()
		case ">":
			v.hOffset += 4
		}
	}
	return v, nil
}

func (v *ReviewPanelView) View() string {
	leftW := v.leftW()
	rightW := v.rightW()
	bodyH := v.bodyH()

	// ── Left pane: task list ───────────────────────────────────────────────
	var allTaskLines []string
	for i, task := range v.tasks {
		titleMaxW := leftW - 5
		if titleMaxW < 1 {
			titleMaxW = 1
		}
		title := task.Title
		if len([]rune(title)) > titleMaxW {
			title = string([]rune(title)[:titleMaxW]) + "…"
		}
		icon := "○"
		if task.CompletedAt != nil {
			icon = "✓"
		}
		text := fmt.Sprintf("%d.%s %s", task.Position, icon, title)
		var line string
		if task.CommitHash == "" {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(text)
		} else {
			line = text
		}
		if i == v.taskCursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		allTaskLines = append(allTaskLines, line)
	}
	// Render only the visible window of tasks (respects taskListOffset for scrolling).
	leftDisplay := make([]string, bodyH)
	for i := range leftDisplay {
		idx := v.taskListOffset + i
		if idx < len(allTaskLines) {
			leftDisplay[i] = allTaskLines[idx]
		}
	}
	leftContent := strings.Join(leftDisplay, "\n")
	leftPane := lipgloss.NewStyle().Width(leftW).Height(bodyH).Render(leftContent)

	// ── Right pane: diff ──────────────────────────────────────────────────
	end := v.offset + bodyH
	if end > len(v.lines) {
		end = len(v.lines)
	}
	var rightLines []string
	start := v.offset
	if start > len(v.lines) {
		start = len(v.lines)
	}
	for _, rl := range v.lines[start:end] {
		var displayLine string
		if rl.rawText != "" {
			// Apply horizontal offset: slice the tab-expanded raw text, then style.
			runes := []rune(rl.rawText)
			var visible string
			if v.hOffset < len(runes) {
				endIdx := v.hOffset + rightW
				if endIdx > len(runes) {
					endIdx = len(runes)
				}
				visible = string(runes[v.hOffset:endIdx])
			}
			displayLine = applyLineKind(rl.lineKind, visible)
		} else {
			displayLine = rl.text
		}
		rightLines = append(rightLines, displayLine)
	}
	for len(rightLines) < bodyH {
		rightLines = append(rightLines, "")
	}
	rightContent := strings.Join(rightLines, "\n")

	rightPane := lipgloss.NewStyle().
		Width(rightW).
		Height(bodyH).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("4")).
		Render(rightContent)

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	hint := "[↑↓/jk] tasks · [[] up · []] down · [<][>] scroll h · [n] hunk · [c] comment · [v] threads · [a] approve · [S] submit · [esc] back"
	hintLine := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(hint)

	return body + "\n" + hintLine
}
