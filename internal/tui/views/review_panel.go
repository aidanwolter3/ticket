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

// leftItemKind identifies the type of an item in the left pane.
type leftItemKind int

const (
	leftKindTask         leftItemKind = iota
	leftKindThread                    // real submitted thread
	leftKindDraftThread               // staged (not yet submitted) draft thread
	leftKindMessage                   // individual message inside an expanded thread
	leftKindDraftMessage              // draft reply to a real thread
)

// leftItem is one navigable entry in the left pane flat list.
type leftItem struct {
	kind         leftItemKind
	taskIdx      int
	thread       *model.Thread
	draftThread  *model.DraftThread
	draftMsg     *model.DraftMessage // for leftKindDraftMessage: draft reply to a real thread
	stagedAction string              // "resolve"/"reopen"/"" for thread items
	msgIdx       int                 // for leftKindMessage: index into thread.Messages
}

// ReviewPanelView is the full-screen code-review split-pane overlay.
type ReviewPanelView struct {
	store      *store.Store
	ticket     *model.Ticket
	tasks      []model.Task
	threads    []*model.Thread
	draftState *model.DraftState

	// Left pane state.
	leftItems      []leftItem
	leftCursor     int
	leftListOffset int
	expandedTasks  map[string]bool // taskID → threads shown
	expandedThread string          // thread ID whose messages are shown inline

	// Right pane state (diff).
	lines   []renderedLine
	offset  int
	hOffset int // horizontal scroll offset

	width  int
	height int
}

func NewReviewPanelView(s *store.Store, ticketID string) (*ReviewPanelView, error) {
	v := &ReviewPanelView{
		store:         s,
		expandedTasks: make(map[string]bool),
	}
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

	v.buildLeftItems()
	v.buildDiffLines()
	return nil
}

func (v *ReviewPanelView) Reload() error {
	if v.ticket == nil {
		return nil
	}
	return v.load(v.ticket.ID)
}

func (v *ReviewPanelView) TicketID() string {
	if v.ticket == nil {
		return ""
	}
	return v.ticket.ID
}

// selectedTaskIndex returns the taskIdx for the current left cursor.
func (v *ReviewPanelView) selectedTaskIndex() int {
	if v.leftCursor < len(v.leftItems) {
		return v.leftItems[v.leftCursor].taskIdx
	}
	return 0
}

func (v *ReviewPanelView) SelectedTaskID() string {
	idx := v.selectedTaskIndex()
	if idx < len(v.tasks) {
		return v.tasks[idx].ID
	}
	return ""
}

// SelectedDraftMessage returns the draft message at the current cursor, or nil.
// Handles both draft thread messages and draft reply messages.
func (v *ReviewPanelView) SelectedDraftMessage() *model.DraftMessage {
	if v.leftCursor >= len(v.leftItems) {
		return nil
	}
	item := v.leftItems[v.leftCursor]
	switch item.kind {
	case leftKindDraftThread:
		if item.draftThread != nil && len(item.draftThread.Messages) > 0 {
			return &item.draftThread.Messages[0]
		}
	case leftKindDraftMessage:
		return item.draftMsg
	}
	return nil
}

// SelectedThread returns the real thread at the current left cursor, or nil.
// Also returns the thread when a message inside it is selected.
func (v *ReviewPanelView) SelectedThread() *model.Thread {
	if v.leftCursor < len(v.leftItems) {
		item := v.leftItems[v.leftCursor]
		if item.kind == leftKindThread || item.kind == leftKindMessage {
			return item.thread
		}
	}
	return nil
}

// threadsByTask returns a map of taskID → threads for quick lookup.
func (v *ReviewPanelView) threadsByTask() map[string][]*model.Thread {
	m := make(map[string][]*model.Thread)
	for _, th := range v.threads {
		m[th.TaskID] = append(m[th.TaskID], th)
	}
	return m
}

// draftThreadsByTask returns a map of taskID → draft threads for quick lookup.
func (v *ReviewPanelView) draftThreadsByTask() map[string][]*model.DraftThread {
	m := make(map[string][]*model.DraftThread)
	if v.draftState == nil {
		return m
	}
	for i := range v.draftState.NewThreads {
		dt := &v.draftState.NewThreads[i]
		m[dt.TaskID] = append(m[dt.TaskID], dt)
	}
	return m
}

// buildLeftItems rebuilds the flat left-pane item list from current state.
func (v *ReviewPanelView) buildLeftItems() {
	tbt := v.threadsByTask()
	dtbt := v.draftThreadsByTask()
	v.leftItems = nil
	for i, task := range v.tasks {
		v.leftItems = append(v.leftItems, leftItem{kind: leftKindTask, taskIdx: i})
		if !v.expandedTasks[task.ID] {
			continue
		}
		for _, th := range tbt[task.ID] {
			staged := ""
			if v.draftState != nil {
				staged = v.draftState.ActionFor(th.ID)
			}
			v.leftItems = append(v.leftItems, leftItem{
				kind:         leftKindThread,
				taskIdx:      i,
				thread:       th,
				stagedAction: staged,
			})
			if v.expandedThread == th.ID {
				for mi := range th.Messages {
					v.leftItems = append(v.leftItems, leftItem{
						kind:    leftKindMessage,
						taskIdx: i,
						thread:  th,
						msgIdx:  mi,
					})
				}
				if v.draftState != nil {
					for _, dm := range v.draftState.RepliesFor(th.ID) {
						dmCopy := dm
						v.leftItems = append(v.leftItems, leftItem{
							kind:     leftKindDraftMessage,
							taskIdx:  i,
							thread:   th,
							draftMsg: &dmCopy,
						})
					}
				}
			}
		}
		for _, dt := range dtbt[task.ID] {
			v.leftItems = append(v.leftItems, leftItem{
				kind:        leftKindDraftThread,
				taskIdx:     i,
				draftThread: dt,
			})
		}
	}
	if v.leftCursor >= len(v.leftItems) {
		v.leftCursor = max(0, len(v.leftItems)-1)
	}
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
	if idx < len(v.lines) {
		return v.lines[idx].filePath, v.lines[idx].hunkHdr
	}
	return "", ""
}

func (v *ReviewPanelView) buildDiffLines() {
	v.lines = nil
	v.offset = 0
	v.hOffset = 0
	taskIdx := v.selectedTaskIndex()
	if taskIdx >= len(v.tasks) {
		return
	}
	task := v.tasks[taskIdx]
	rw := v.rightW()

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
	htMap := v.threadsByHunk()

	currentFile := ""
	currentHunk := ""

	flushHunkAnnotations := func(fp, hh string) {
		k := fp + "\x00" + hh
		ht := htMap[k]
		if ht == nil {
			return
		}
		rw := v.rightW()
		for _, th := range ht.real {
			msgCount := fmt.Sprintf("(%d msg)", len(th.Messages))
			// "    ┆ " = 6, icon = 1, " " = 1, " " = 1, msgCount
			maxSummaryW := rw - 6 - 1 - 1 - 1 - len([]rune(msgCount))
			if maxSummaryW > 60 {
				maxSummaryW = 60
			}
			summary := th.Summary()
			if len([]rune(summary)) > maxSummaryW {
				summary = string([]rune(summary)[:maxSummaryW]) + "…"
			}
			icon := components.ThreadStatusIcon(th.Status)
			line := fmt.Sprintf("    ┆ %s %s %s", icon, summary,
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(msgCount))
			v.lines = append(v.lines, renderedLine{text: line, filePath: fp, hunkHdr: hh, threadID: th.ID})
		}
		for _, dt := range ht.draft {
			// "    ┆ " = 6, icon = 1, " " = 1, " " = 1, "[draft]" = 7
			maxSummaryW := rw - 6 - 1 - 1 - 1 - 7
			if maxSummaryW > 60 {
				maxSummaryW = 60
			}
			if maxSummaryW < 1 {
				maxSummaryW = 1
			}
			summary := "(empty draft)"
			if len(dt.Messages) > 0 {
				s := strings.SplitN(dt.Messages[0].Text, "\n", 2)[0]
				if len([]rune(s)) > maxSummaryW {
					s = string([]rune(s)[:maxSummaryW]) + "…"
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
			flushHunkAnnotations(currentFile, currentHunk)
			currentHunk = raw
			isHunk = true
		}

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

		expanded := strings.ReplaceAll(raw, "\t", "    ")
		v.lines = append(v.lines, renderedLine{rawText: expanded, lineKind: lk, filePath: currentFile, hunkHdr: currentHunk, isHunk: isHunk})
	}
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

func (v *ReviewPanelView) updateLeftListOffset() {
	bh := v.bodyH()
	if v.leftCursor < v.leftListOffset {
		v.leftListOffset = v.leftCursor
	}
	if v.leftCursor >= v.leftListOffset+bh {
		// Pin selected item to top so expanded thread messages scroll into view.
		v.leftListOffset = v.leftCursor
	}
	if v.leftListOffset < 0 {
		v.leftListOffset = 0
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

func (v *ReviewPanelView) jumpToPrevHunk() {
	for i := v.offset - 1; i >= 0; i-- {
		if v.lines[i].isHunk {
			v.offset = i
			v.clampOffset()
			return
		}
	}
}

// toggleThreadExpansion expands or collapses threads for the task at the current cursor.
func (v *ReviewPanelView) toggleThreadExpansion() {
	if v.leftCursor >= len(v.leftItems) {
		return
	}
	item := v.leftItems[v.leftCursor]
	taskID := v.tasks[item.taskIdx].ID
	v.expandedTasks[taskID] = !v.expandedTasks[taskID]
	v.buildLeftItems()
	v.updateLeftListOffset()
}

func (v *ReviewPanelView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		prevTaskIdx := v.selectedTaskIndex()
		switch km.String() {
		case "up", "k":
			if v.leftCursor > 0 {
				v.leftCursor--
				v.updateLeftListOffset()
			}
		case "down", "j":
			if v.leftCursor < len(v.leftItems)-1 {
				v.leftCursor++
				v.updateLeftListOffset()
			}
		case "enter":
			if v.leftCursor < len(v.leftItems) {
				item := v.leftItems[v.leftCursor]
				if item.kind == leftKindTask {
					v.toggleThreadExpansion()
				} else if item.kind == leftKindThread {
					thID := item.thread.ID
					wasExpanded := v.expandedThread == thID
					if wasExpanded {
						v.expandedThread = ""
					} else {
						v.expandedThread = thID
					}
					v.buildLeftItems()
					// Always reposition cursor on the thread item itself.
					for i, li := range v.leftItems {
						if li.kind == leftKindThread && li.thread != nil && li.thread.ID == thID {
							v.leftCursor = i
							break
						}
					}
					v.updateLeftListOffset()
				}
				// leftKindMessage: enter does nothing
			}
		case "[":
			v.offset--
			v.clampOffset()
		case "]":
			v.offset++
			v.clampOffset()
		case "n":
			v.jumpToNextHunk()
		case "N":
			v.jumpToPrevHunk()
		case "<":
			v.hOffset -= 4
			v.clampHOffset()
		case ">":
			v.hOffset += 4
		}
		// Rebuild diff only when task selection changes.
		if v.selectedTaskIndex() != prevTaskIdx {
			v.buildDiffLines()
		}
	}
	return v, nil
}

func (v *ReviewPanelView) View() string {
	leftW := v.leftW()
	rightW := v.rightW()
	bodyH := v.bodyH()

	// ── Left pane: task list with inline threads ───────────────────────────
	tbt := v.threadsByTask()
	dtbt := v.draftThreadsByTask()
	var allLeftLines []string
	for idx, item := range v.leftItems {
		switch item.kind {
		case leftKindTask:
			task := v.tasks[item.taskIdx]
			realThreads := tbt[task.ID]
			draftCnt := len(dtbt[task.ID])
			totalThreads := len(realThreads) + draftCnt

			// Count real threads by open vs resolved.
			var openCnt, resolvedCnt int
			for _, th := range realThreads {
				if th.Status == model.ThreadResolved {
					resolvedCnt++
				} else {
					openCnt++
				}
			}

			// Build suffix showing expand marker and open/resolved counts.
			var suffix string
			var suffixVisLen int
			if totalThreads > 0 {
				marker := "▶"
				if v.expandedTasks[task.ID] {
					marker = "▼"
				}
				plainSuffix := " " + marker
				styledSuffix := " " + marker
				if openCnt > 0 {
					plainSuffix += fmt.Sprintf(" %d●", openCnt)
					styledSuffix += " " + lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(fmt.Sprintf("%d●", openCnt))
				}
				if resolvedCnt > 0 {
					plainSuffix += fmt.Sprintf(" %d✓", resolvedCnt)
					styledSuffix += " " + lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(fmt.Sprintf("%d✓", resolvedCnt))
				}
				if draftCnt > 0 {
					plainSuffix += fmt.Sprintf(" %d◌", draftCnt)
					styledSuffix += " " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(fmt.Sprintf("%d◌", draftCnt))
				}
				suffix = styledSuffix
				suffixVisLen = len([]rune(plainSuffix))
			}

			titleMaxW := leftW - 5 - suffixVisLen
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
				line = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(text) + suffix
			} else {
				line = text + suffix
			}
			if idx == v.leftCursor {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			}
			allLeftLines = append(allLeftLines, line)

		case leftKindThread:
			th := item.thread
			icon := v.threadIcon(th.Status, item.stagedAction)

			var suffix, suffixPlain string
			switch item.stagedAction {
			case model.DraftActionResolve:
				suffixPlain = " [→resolved]"
				suffix = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[→resolved]")
			case model.DraftActionReopen:
				suffixPlain = " [→open]"
				suffix = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[→open]")
			}

			msgCountPlain := fmt.Sprintf("(%d)", len(th.Messages))
			// Available width: leftW minus indent(2) + icon(1) + space(1) + suffix + space(1) + msgCount.
			summaryW := leftW - 4 - len([]rune(suffixPlain)) - 1 - len([]rune(msgCountPlain))
			if summaryW < 1 {
				summaryW = 1
			}
			summary := th.Summary()
			if len([]rune(summary)) > summaryW {
				summary = string([]rune(summary)[:summaryW]) + "…"
			}

			line := fmt.Sprintf("  %s %s%s %s", icon, summary, suffix,
				lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(msgCountPlain))

			if idx == v.leftCursor {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			}
			allLeftLines = append(allLeftLines, line)

		case leftKindMessage:
			th := item.thread
			msg := th.Messages[item.msgIdx]
			author := lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render(msg.Author)
			// Format: "      <author>: <firstLine>" — indent(6) + author + ": "(2) = 8 overhead
			// maxW is the max allowed firstLine length including any appended "…"
			maxW := leftW - 8 - len(msg.Author)
			if maxW < 2 {
				maxW = 2
			}
			firstLine := strings.SplitN(msg.Text, "\n", 2)[0]
			runes := []rune(firstLine)
			if len(runes) > maxW {
				firstLine = string(runes[:maxW-1]) + "…"
			}
			line := fmt.Sprintf("      %s: %s", author, firstLine)
			if idx == v.leftCursor {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			}
			allLeftLines = append(allLeftLines, line)

		case leftKindDraftThread:
			dt := item.draftThread
			icon := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("◌")
			summary := "(empty draft)"
			if len(dt.Messages) > 0 {
				summary = strings.SplitN(dt.Messages[0].Text, "\n", 2)[0]
			}
			summaryW := leftW - 12 // indent(2) + icon(1) + space(1) + " [draft]"(7) + "…"(1) = 12
			if summaryW < 2 {
				summaryW = 2
			}
			if len([]rune(summary)) > summaryW {
				summary = string([]rune(summary)[:summaryW-1]) + "…"
			}
			draftLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[draft]")
			line := fmt.Sprintf("  %s %s %s", icon, summary, draftLabel)
			if idx == v.leftCursor {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			}
			allLeftLines = append(allLeftLines, line)

		case leftKindDraftMessage:
			dm := item.draftMsg
			if dm == nil {
				break
			}
			authorStr := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(dm.Author + " [draft]")
			maxW := leftW - 8 - len([]rune(dm.Author)) - 8
			if maxW < 2 {
				maxW = 2
			}
			firstLine := strings.SplitN(dm.Text, "\n", 2)[0]
			runes := []rune(firstLine)
			if len(runes) > maxW {
				firstLine = string(runes[:maxW-1]) + "…"
			}
			line := fmt.Sprintf("      %s: %s", authorStr, firstLine)
			if idx == v.leftCursor {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			}
			allLeftLines = append(allLeftLines, line)
		}
	}

	// Render only the visible window (respects leftListOffset for scrolling).
	// Cap each line to leftW to prevent lipgloss from wrapping overlong lines.
	leftDisplay := make([]string, bodyH)
	for i := range leftDisplay {
		idx := v.leftListOffset + i
		if idx < len(allLeftLines) {
			leftDisplay[i] = lipgloss.NewStyle().MaxWidth(leftW).Render(allLeftLines[idx])
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

	hint := "[↑↓/jk] navigate · [enter] expand · [r] reply · [x] resolve · [e] edit draft · [[]] v-scroll · [<>] h-scroll · [n/N] hunk · [c] comment · [a] approve · [ctrl+s] submit · [esc] back"
	hintLine := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(hint)

	return body + "\n" + hintLine
}

// threadIcon returns the display icon for a thread, taking staged action into account.
func (v *ReviewPanelView) threadIcon(status model.ThreadStatus, staged string) string {
	if staged == model.DraftActionResolve {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("✓")
	}
	if staged == model.DraftActionReopen {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("●")
	}
	return components.ThreadStatusIcon(status)
}
