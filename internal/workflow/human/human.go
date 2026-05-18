package human

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/aidanwolter/ticket/internal/agent"
	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

// Draft creates a new ticket in draft status.
func Draft(s *store.Store, title, description, repoPath, configName string) (*model.Ticket, error) {
	t := &model.Ticket{
		Title:       title,
		Type:        model.TypeTicket,
		Status:      model.StatusDraft,
		Description: description,
		RepoPath:    repoPath,
		Config:      configName,
	}
	if err := s.CreateTicket(t); err != nil {
		return nil, fmt.Errorf("draft: %w", err)
	}
	return t, nil
}

// Delete deletes a ticket. Returns an error if the ticket is in_progress or later.
func Delete(s *store.Store, ticketID string) error {
	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	switch ticket.Status {
	case model.StatusPreparing, model.StatusTearingDown,
		model.StatusInProgress, model.StatusInReview, model.StatusApproved, model.StatusMerged:
		return fmt.Errorf("delete: cannot delete ticket %s with status %s", ticketID, ticket.Status)
	}
	if err := s.DeleteTicket(ticketID); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}

// Update updates the title and/or description of a ticket. Pass nil to leave a field unchanged.
func Update(s *store.Store, ticketID string, title, description *string) error {
	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if title != nil {
		ticket.Title = *title
	}
	if description != nil {
		ticket.Description = *description
	}
	if err := s.UpdateTicket(ticket); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	return nil
}

// Dispatch creates a workspace and launches an agent for the ticket. The ticket
// must already be in "ready" status. On workspace creation failure the ticket
// reverts to ready and an error is returned. Dispatch is called by both Ready()
// (auto-dispatch) and the TUI manual dispatch flow.
func Dispatch(s *store.Store, ticketID string, launcher *agent.Launcher, stdout, stderr io.Writer) error {
	cmdTemplate, _, err := s.ConfigGet("agent.command")
	if err != nil {
		return fmt.Errorf("dispatch: read agent.command: %w", err)
	}
	if cmdTemplate == "" {
		return fmt.Errorf("dispatch: agent.command not configured")
	}

	args, err := agent.BuildPrompt(cmdTemplate)
	if err != nil {
		return fmt.Errorf("dispatch: build prompt: %w", err)
	}

	ws, err := NewWorkspace(s)
	if err != nil {
		return fmt.Errorf("dispatch: workspace config: %w", err)
	}

	if transErr := s.TransitionTicket(ticketID, model.StatusPreparing); transErr != nil {
		return fmt.Errorf("dispatch: transition preparing: %w", transErr)
	}

	wsPath, createErr := ws.Create(ticketID, stdout, stderr)
	if createErr != nil {
		s.TransitionTicket(ticketID, model.StatusReady) //nolint:errcheck
		return fmt.Errorf("dispatch: create workspace: %w", createErr)
	}

	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		s.TransitionTicket(ticketID, model.StatusReady) //nolint:errcheck
		return fmt.Errorf("dispatch: get ticket: %w", err)
	}
	if ticket.WorktreePath == "" {
		ticket.WorktreePath = wsPath
	}

	if launcher == nil {
		launcher = agent.NewLauncher(s)
	}
	if _, launchErr := launcher.Launch(ticketID, ticket.WorktreePath, args); launchErr != nil {
		s.TransitionTicket(ticketID, model.StatusReady) //nolint:errcheck
		return fmt.Errorf("dispatch: launch agent: %w", launchErr)
	}

	if transErr := s.TransitionTicket(ticketID, model.StatusInProgress); transErr != nil {
		fmt.Fprintf(stderr, "dispatch: transition in_progress: %v\n", transErr)
	}
	if _, noteErr := s.AddNote(ticketID, "agent:claude", "Agent auto-dispatched"); noteErr != nil {
		fmt.Fprintf(stderr, "dispatch: add note: %v\n", noteErr)
	}
	return nil
}

// Ready transitions a draft ticket to ready. If agent.auto_dispatch is true
// and agent.command is set, an agent is launched automatically using launcher.
// If launcher is nil, a new one is created (suitable for CLI use where no TUI
// attach is expected).
func Ready(s *store.Store, ticketID string, launcher *agent.Launcher, stdout, stderr io.Writer) error {
	ticket, err := s.ReadyTicket(ticketID)
	if err != nil {
		return fmt.Errorf("ready: %w", err)
	}

	autoDispatch, _, _ := s.ConfigGet("agent.auto_dispatch")
	cmdTemplate, _, _ := s.ConfigGet("agent.command")
	if autoDispatch == "true" && cmdTemplate != "" {
		for _, blockerID := range ticket.BlockedBy {
			blocker, bErr := s.GetTicket(blockerID)
			if bErr != nil {
				fmt.Fprintf(stderr, "auto-dispatch: skipped — could not load blocker %s: %v\n", blockerID, bErr)
				return nil
			}
			if blocker.Status != model.StatusApproved && blocker.Status != model.StatusMerged {
				fmt.Fprintf(stderr, "auto-dispatch: skipped — blocker %s is %s (need approved or merged)\n", blockerID, blocker.Status)
				return nil
			}
		}
		existing, _ := s.GetAgentSessionByTicket(ticketID)
		if existing == nil {
			if launcher == nil {
				launcher = agent.NewLauncher(s)
			}
			if dispErr := Dispatch(s, ticketID, launcher, stdout, stderr); dispErr != nil {
				fmt.Fprintf(stderr, "auto-dispatch: workspace: %v\n", dispErr)
			}
		}
	}

	return nil
}

// SubmitReview flushes all staged draft actions to the store atomically,
// auto-generates amendment tasks for each needs_attention thread, and
// transitions the ticket from in_review to ready only when at least one
// needs_attention thread exists. If all actions were resolutions, the ticket
// stays in_review. If launcher is non-nil and agent.auto_dispatch is set,
// an agent is launched after the transition. Pass nil launcher to skip.
// stdout and stderr are accepted for interface symmetry but are unused; pass io.Discard.
func SubmitReview(s *store.Store, ticketID string, author string, launcher *agent.Launcher, stdout, stderr io.Writer) error {
	naThreadIDs, err := s.FlushDraftState(ticketID)
	if err != nil {
		return fmt.Errorf("submit-review: flush draft: %w", err)
	}

	if len(naThreadIDs) == 0 {
		return nil
	}

	if err := createAmendmentTasks(s, ticketID, naThreadIDs); err != nil {
		return fmt.Errorf("submit-review: create amendment tasks: %w", err)
	}

	if err := s.TransitionTicket(ticketID, model.StatusReady); err != nil {
		return fmt.Errorf("submit-review: transition ticket: %w", err)
	}

	if launcher != nil {
		autoDispatch, _, _ := s.ConfigGet("agent.auto_dispatch")
		cmdTemplate, _, _ := s.ConfigGet("agent.command")
		if autoDispatch == "true" && cmdTemplate != "" {
			existing, _ := s.GetAgentSessionByTicket(ticketID)
			if existing == nil {
				if dispErr := Dispatch(s, ticketID, launcher, stdout, stderr); dispErr != nil {
					fmt.Fprintf(stderr, "auto-dispatch: %v\n", dispErr)
				}
			}
		}
	}

	return nil
}

// createAmendmentTasks creates round-N tasks for each needs_attention thread.
func createAmendmentTasks(s *store.Store, ticketID string, naThreadIDs []string) error {
	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		return err
	}

	// Determine next round number.
	maxRound := 1
	for _, task := range ticket.Tasks {
		if task.Round > maxRound {
			maxRound = task.Round
		}
	}
	nextRound := maxRound + 1

	// Determine next position.
	maxPosition := 0
	for _, task := range ticket.Tasks {
		if task.Position > maxPosition {
			maxPosition = task.Position
		}
	}

	for i, threadID := range naThreadIDs {
		th, err := s.GetThread(threadID)
		if err != nil {
			return fmt.Errorf("get thread %s: %w", threadID, err)
		}
		title := th.Summary()
		task := &model.Task{
			TicketID:    ticketID,
			Title:       title,
			Description: fmt.Sprintf("Address review thread %s", threadID),
			Position:    maxPosition + i + 1,
			Round:       nextRound,
		}
		if err := s.CreateTask(task); err != nil {
			return fmt.Errorf("create amendment task for thread %s: %w", threadID, err)
		}
	}
	return nil
}

// Redraft tears down the workspace, deletes the feature branch, resets tasks,
// and transitions the ticket back to draft. Handles crash recovery for
// StatusPreparing (skips Delete) and sticky-failure retry for StatusTearingDown.
func Redraft(s *store.Store, ticketID string, stdout, stderr io.Writer) error {
	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		return fmt.Errorf("redraft: %w", err)
	}

	if ticket.Status == model.StatusDraft {
		return fmt.Errorf("ticket %s is already draft", ticketID)
	}

	// Terminate any running agent session.
	if sess, sErr := s.GetAgentSessionByTicket(ticketID); sErr == nil && sess != nil {
		if proc, pErr := os.FindProcess(sess.PID); pErr == nil {
			proc.Signal(syscall.SIGTERM) //nolint:errcheck
		}
		s.UpdateAgentSessionState(sess.ID, model.AgentTerminated) //nolint:errcheck
	}

	ws, wsErr := NewWorkspace(s)

	// Crash recovery: workspace creation was interrupted; skip Delete.
	if ticket.Status == model.StatusPreparing {
		if clearErr := s.ClearWorktree(ticketID); clearErr != nil {
			fmt.Fprintf(stderr, "redraft: crash recovery: clear worktree: %v\n", clearErr)
			return clearErr
		}
		if resetErr := s.ResetTasksForTicket(ticketID); resetErr != nil {
			fmt.Fprintf(stderr, "redraft: crash recovery: reset tasks: %v\n", resetErr)
			return resetErr
		}
		if transErr := s.TransitionTicket(ticketID, model.StatusDraft); transErr != nil {
			fmt.Fprintf(stderr, "redraft: crash recovery: transition draft: %v\n", transErr)
			return transErr
		}
		fmt.Fprintf(stderr, "crash recovery: force-transitioning to draft\n")
		return nil
	}

	// Transition to tearing_down (skip if already there).
	if ticket.Status != model.StatusTearingDown {
		if transErr := s.TransitionTicket(ticketID, model.StatusTearingDown); transErr != nil {
			return fmt.Errorf("redraft: transition tearing_down: %w", transErr)
		}
	}

	if wsErr != nil {
		return fmt.Errorf("redraft: workspace config: %w", wsErr)
	}

	if delErr := ws.Delete(ticketID, stdout, stderr); delErr != nil {
		fmt.Fprintf(stderr, "redraft: workspace delete failed: %v\n", delErr)
		return delErr
	}

	if ticket.FeatureBranch != "" {
		repoPath := ticket.RepoPath
		delCmd := exec.Command("git", "-C", repoPath, "branch", "-D", ticket.FeatureBranch)
		delCmd.Stdout = stdout
		delCmd.Stderr = stderr
		if err := delCmd.Run(); err != nil {
			fmt.Fprintf(stderr, "redraft: warning: could not delete branch %s: %v\n", ticket.FeatureBranch, err)
		}
	}

	if err := s.ClearWorktree(ticketID); err != nil {
		return fmt.Errorf("redraft: clear worktree_path: %w", err)
	}

	if err := s.ResetTasksForTicket(ticketID); err != nil {
		return fmt.Errorf("redraft: reset tasks: %w", err)
	}

	if err := s.TransitionTicket(ticketID, model.StatusDraft); err != nil {
		return fmt.Errorf("redraft: transition: %w", err)
	}
	return nil
}

// MarkMerged runs the workspace delete command and transitions the ticket to
// merged. Precondition: ticket must be in tearing_down status. On Delete
// failure the ticket reverts to approved.
func MarkMerged(s *store.Store, ticketID string, stdout, stderr io.Writer) error {
	ws, err := NewWorkspace(s)
	if err != nil {
		return fmt.Errorf("mark-merged: workspace config: %w", err)
	}

	if delErr := ws.Delete(ticketID, stdout, stderr); delErr != nil {
		s.TransitionTicket(ticketID, model.StatusApproved) //nolint:errcheck
		return delErr
	}

	if transErr := s.TransitionTicket(ticketID, model.StatusMerged); transErr != nil {
		return fmt.Errorf("mark-merged: transition merged: %w", transErr)
	}
	return nil
}

// Merge ff-merges the feature branch into main, removes the worktree, deletes
// the branch, and transitions the ticket to merged. stdout and stderr control
// where git output goes; pass io.Discard to suppress (e.g. from a TUI).
func Merge(s *store.Store, ticketID string, stdout, stderr io.Writer) error {
	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		return fmt.Errorf("merge: %w", err)
	}

	if ticket.Status != model.StatusApproved {
		return fmt.Errorf("merge: ticket %s is %s, not approved", ticketID, ticket.Status)
	}

	for _, task := range ticket.Tasks {
		if task.CompletedAt == nil {
			return fmt.Errorf("merge: task %s (%q) is not complete", task.ID, task.Title)
		}
	}

	for _, task := range ticket.Tasks {
		threads, err := s.GetThreadsForTask(task.ID)
		if err != nil {
			return fmt.Errorf("merge: load threads: %w", err)
		}
		for _, th := range threads {
			if th.Status == model.ThreadOpen || th.Status == model.ThreadNeedsAttention {
				return fmt.Errorf("merge: ticket %s has open thread %s — resolve all threads before merging", ticketID, th.ID)
			}
		}
	}

	repoPath := ticket.RepoPath
	if repoPath == "" {
		return fmt.Errorf("merge: ticket %s has no repo_path set", ticketID)
	}
	if _, err := os.Stat(repoPath); err != nil {
		return fmt.Errorf("merge: repo_path %q does not exist: %w", repoPath, err)
	}

	if ticket.FeatureBranch == "" {
		return fmt.Errorf("merge: ticket %s has no feature_branch set", ticketID)
	}
	featureBranch := ticket.FeatureBranch

	var mergeStderr bytes.Buffer
	mergeCmd := exec.Command("git", "-C", repoPath, "merge", "--ff-only", featureBranch)
	mergeCmd.Stdout = stdout
	mergeCmd.Stderr = io.MultiWriter(stderr, &mergeStderr)
	if err := mergeCmd.Run(); err != nil {
		// Check if divergence is the cause (feature branch is not an ancestor of HEAD).
		isAncestorCmd := exec.Command("git", "-C", repoPath, "merge-base", "--is-ancestor", featureBranch, "HEAD")
		if isAncestorCmd.Run() != nil {
			// Feature branch has diverged — auto-rebase onto current HEAD branch.
			headBranchOut, hbErr := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD").Output()
			if hbErr != nil {
				return fmt.Errorf("merge: branch has diverged from main — rebase manually then retry")
			}
			headBranch := strings.TrimSpace(string(headBranchOut))

			var rebaseCmd *exec.Cmd
			if ticket.WorktreePath != "" {
				rebaseCmd = exec.Command("git", "-C", ticket.WorktreePath, "rebase", headBranch)
			} else {
				rebaseCmd = exec.Command("git", "-C", repoPath, "rebase", headBranch, featureBranch)
			}
			rebaseCmd.Stdout = stdout
			rebaseCmd.Stderr = stderr
			if rbErr := rebaseCmd.Run(); rbErr != nil {
				location := ticket.WorktreePath
				if location == "" {
					location = repoPath
				}
				return fmt.Errorf("merge: rebase produced conflicts — resolve them in %s and retry ticket merge", location)
			}

			// Retry ff-merge after successful rebase.
			var retryStderr bytes.Buffer
			retryCmd := exec.Command("git", "-C", repoPath, "merge", "--ff-only", featureBranch)
			retryCmd.Stdout = stdout
			retryCmd.Stderr = io.MultiWriter(stderr, &retryStderr)
			if err := retryCmd.Run(); err != nil {
				if msg := strings.TrimSpace(retryStderr.String()); msg != "" {
					return fmt.Errorf("merge: %s", msg)
				}
				return fmt.Errorf("merge: merge failed after rebase")
			}
		} else {
			if msg := strings.TrimSpace(mergeStderr.String()); msg != "" {
				return fmt.Errorf("merge: %s", msg)
			}
			return fmt.Errorf("merge: branch has diverged from main — rebase manually then retry")
		}
	}

	ws := WorktreeWorkspace{s: s}
	if err := ws.Delete(ticketID, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "merge: warning: could not clean up workspace: %v\n", err)
	}

	delCmd := exec.Command("git", "-C", repoPath, "branch", "-d", featureBranch)
	delCmd.Stdout = stdout
	delCmd.Stderr = stderr
	if err := delCmd.Run(); err != nil {
		fmt.Fprintf(stderr, "merge: warning: could not delete branch %s: %v\n", featureBranch, err)
	}

	ticket.WorktreePath = ""
	ticket.FeatureBranch = ""
	if err := s.UpdateTicket(ticket); err != nil {
		return fmt.Errorf("merge: update ticket: %w", err)
	}

	if err := s.TransitionTicket(ticketID, model.StatusMerged); err != nil {
		return fmt.Errorf("merge: transition: %w", err)
	}

	return nil
}
