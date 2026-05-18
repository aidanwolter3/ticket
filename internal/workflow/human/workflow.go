package human

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/aidanwolter/ticket/internal/agent"
	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

// Workflow owns the store and exposes all operations that CLI and TUI layers
// need.  No code outside internal/workflow/* should hold a *store.Store.
type Workflow struct {
	s        *store.Store
	launcher *agent.Launcher
}

// New wraps an open store in a Workflow.  The caller is responsible for
// closing the store (defer s.Close()) after the Workflow is no longer needed.
func New(s *store.Store) *Workflow {
	return &Workflow{s: s, launcher: agent.NewLauncher(s)}
}

// Launcher returns the agent launcher managed by this workflow.
func (w *Workflow) Launcher() *agent.Launcher {
	return w.launcher
}

// --- human workflow operations ---

func (w *Workflow) Draft(title, description, repoPath string) (*model.Ticket, error) {
	return Draft(w.s, title, description, repoPath)
}

func (w *Workflow) Delete(ticketID string) error {
	return Delete(w.s, ticketID)
}

func (w *Workflow) Update(ticketID string, title, description *string) error {
	return Update(w.s, ticketID, title, description)
}

func (w *Workflow) Dispatch(ticketID string, stdout, stderr io.Writer) error {
	return Dispatch(w.s, ticketID, w.launcher, stdout, stderr)
}

func (w *Workflow) Ready(ticketID string, stdout, stderr io.Writer) error {
	return Ready(w.s, ticketID, w.launcher, stdout, stderr)
}

func (w *Workflow) MarkMerged(ticketID string, stdout, stderr io.Writer) error {
	return MarkMerged(w.s, ticketID, stdout, stderr)
}

func (w *Workflow) SubmitReview(ticketID, author string, stdout, stderr io.Writer) error {
	return SubmitReview(w.s, ticketID, author, w.launcher, stdout, stderr)
}

func (w *Workflow) Redraft(ticketID string, stdout, stderr io.Writer) error {
	return Redraft(w.s, ticketID, stdout, stderr)
}

func (w *Workflow) Merge(ticketID string, stdout, stderr io.Writer) error {
	return Merge(w.s, ticketID, stdout, stderr)
}

func (w *Workflow) AddTask(ticketID, title, description, verifiableResult string, noCommit bool, round int) (*model.Task, error) {
	return AddTask(w.s, ticketID, title, description, verifiableResult, noCommit, round)
}

func (w *Workflow) UpdateTask(taskID string, title, description, verifiableResult *string, noCommit *bool) error {
	return UpdateTask(w.s, taskID, title, description, verifiableResult, noCommit)
}

func (w *Workflow) DeleteTask(taskID string) error {
	return DeleteTask(w.s, taskID)
}

func (w *Workflow) MoveTask(taskID string, position int) error {
	return MoveTask(w.s, taskID, position)
}

func (w *Workflow) ReplyToThread(threadID, author, text string) (*model.Message, error) {
	return ReplyToThread(w.s, threadID, author, text)
}

func (w *Workflow) TransitionThread(threadID string, newStatus model.ThreadStatus, author string) error {
	return TransitionThread(w.s, threadID, newStatus, author)
}

func (w *Workflow) BlockTicket(ticketID, blockerID string) error {
	return BlockTicket(w.s, ticketID, blockerID)
}

func (w *Workflow) UnblockTicket(ticketID, blockerID string) error {
	return UnblockTicket(w.s, ticketID, blockerID)
}

// --- agent workflow operations ---

func (w *Workflow) StartWork(ticketID string) error {
	return w.s.TransitionTicket(ticketID, model.StatusInProgress)
}

func (w *Workflow) SubmitForReview(ticketID string) error {
	return w.s.TransitionTicket(ticketID, model.StatusInReview)
}

func (w *Workflow) CompleteTask(taskID, commitHash string) error {
	task, err := w.s.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("complete task: %w", err)
	}
	if task.NoCommit || commitHash == "" {
		return w.s.CompleteTask(taskID)
	}
	return w.s.CompleteTaskWithCommit(taskID, commitHash)
}

func (w *Workflow) CompleteTaskMostRecentCommit(taskID string) error {
	task, err := w.s.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("complete task: %w", err)
	}
	ticket, err := w.s.GetTicket(task.TicketID)
	if err != nil {
		return fmt.Errorf("complete task: get ticket: %w", err)
	}
	gitPath := ticket.WorktreePath
	if gitPath == "" {
		gitPath = ticket.RepoPath
	}
	if gitPath == "" {
		return fmt.Errorf("complete task: ticket has no worktree_path or repo_path set")
	}
	out, err := exec.Command("git", "-C", gitPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("complete task: git rev-parse HEAD: %w", err)
	}
	return w.CompleteTask(taskID, strings.TrimSpace(string(out)))
}

func (w *Workflow) UncompleteTask(taskID string) error {
	return w.s.UncompleteTask(taskID)
}

// AddNote adds a note to a ticket and returns the note ID.
func (w *Workflow) AddNote(ticketID, author, text string) (string, error) {
	note, err := w.s.AddNote(ticketID, author, text)
	if err != nil {
		return "", err
	}
	return note.ID, nil
}

// SetTaskCommit sets the commit hash on a task without completing it.
func (w *Workflow) SetTaskCommit(taskID, commitHash string) error {
	task, err := w.s.GetTask(taskID)
	if err != nil {
		return err
	}
	task.CommitHash = commitHash
	return w.s.UpdateTask(task)
}

// DeleteAgentSessionsByTicket clears all agent sessions for a ticket.
func (w *Workflow) DeleteAgentSessionsByTicket(ticketID string) error {
	return w.s.DeleteAgentSessionsByTicket(ticketID)
}

// --- direct store reads needed by CLI and TUI ---

func (w *Workflow) GetTicket(id string) (*model.Ticket, error) {
	return w.s.GetTicket(id)
}

func (w *Workflow) GetTask(id string) (*model.Task, error) {
	return w.s.GetTask(id)
}

func (w *Workflow) GetTasksForTicket(ticketID string) ([]model.Task, error) {
	return w.s.GetTasksForTicket(ticketID)
}

func (w *Workflow) GetThreadsForTask(taskID string) ([]*model.Thread, error) {
	return w.s.GetThreadsForTask(taskID)
}

func (w *Workflow) GetAllThreadsForTicket(ticketID string) ([]*model.Thread, error) {
	return w.s.GetAllThreadsForTicket(ticketID)
}

func (w *Workflow) GetNotesForTicket(ticketID string) ([]*model.Note, error) {
	return w.s.GetNotesForTicket(ticketID)
}

func (w *Workflow) ListTickets(filter ...model.Status) ([]*model.Ticket, error) {
	return w.s.ListTickets(filter...)
}

func (w *Workflow) ListBacklogTickets() ([]*model.Ticket, error) {
	return w.s.ListBacklogTickets()
}

func (w *Workflow) SetBacklog(ticketID string, backlog bool) error {
	return w.s.SetBacklog(ticketID, backlog)
}

func (w *Workflow) TransitionTicket(id string, to model.Status) error {
	return w.s.TransitionTicket(id, to)
}

func (w *Workflow) ConfigGet(key string) (string, bool, error) {
	return w.s.ConfigGet(key)
}

func (w *Workflow) ConfigGetDefault(key, defaultVal string) (string, error) {
	return w.s.ConfigGetDefault(key, defaultVal)
}

func (w *Workflow) ConfigSet(key, value string) error {
	return w.s.ConfigSet(key, value)
}

func (w *Workflow) ConfigList() (map[string]string, error) {
	return w.s.ConfigList()
}

func (w *Workflow) NewWorkspace() (Workspace, error) {
	return NewWorkspace(w.s)
}

func (w *Workflow) GetAgentSessionByTicket(ticketID string) (*model.AgentSession, error) {
	return w.s.GetAgentSessionByTicket(ticketID)
}

func (w *Workflow) GetLatestAgentSessionByTicket(ticketID string) (*model.AgentSession, error) {
	return w.s.GetLatestAgentSessionByTicket(ticketID)
}

func (w *Workflow) ListActiveAgentSessions() ([]*model.AgentSession, error) {
	return w.s.ListActiveAgentSessions()
}

func (w *Workflow) GetDraftState(ticketID string) (*model.DraftState, error) {
	return w.s.GetDraftState(ticketID)
}

func (w *Workflow) SetDraftAction(threadID, ticketID, action string) error {
	return w.s.SetDraftAction(threadID, ticketID, action)
}

func (w *Workflow) ClearDraftAction(threadID string) error {
	return w.s.ClearDraftAction(threadID)
}

func (w *Workflow) CreateDraftThread(ticketID, taskID, filePath, hunkHeader string) (*model.DraftThread, error) {
	return w.s.CreateDraftThread(ticketID, taskID, filePath, hunkHeader)
}

func (w *Workflow) AddDraftMessage(threadID, ticketID string, isRealThread bool, author, text string) (*model.DraftMessage, error) {
	return w.s.AddDraftMessage(threadID, ticketID, isRealThread, author, text)
}

func (w *Workflow) UpdateDraftMessage(id, text string) error {
	return w.s.UpdateDraftMessage(id, text)
}

func (w *Workflow) DeleteDraftMessage(id string) error {
	return w.s.DeleteDraftMessage(id)
}

func (w *Workflow) SetWorktreePath(ticketID, worktreePath, repoPath, featureBranch string) error {
	return w.s.SetWorktreePath(ticketID, worktreePath, repoPath, featureBranch)
}
