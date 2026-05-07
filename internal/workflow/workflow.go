package workflow

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

// Promote transitions a draft ticket to ready. stdout and stderr are accepted
// for interface symmetry but are unused; pass io.Discard.
func Promote(s *store.Store, ticketID string, stdout, stderr io.Writer) error {
	if _, err := s.PromoteTicket(ticketID, "human"); err != nil {
		return fmt.Errorf("promote: %w", err)
	}
	return nil
}

// Claim atomically claims the next available work item, creates a git worktree
// for new_work tickets (if worktrees are enabled), and persists worktree_path
// and feature_branch. stdout and stderr control where git output goes; pass
// io.Discard to suppress.
func Claim(s *store.Store, author string, stdout, stderr io.Writer) (*store.WorkItem, error) {
	item, err := s.ClaimWork(author)
	if err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}
	if item == nil {
		return nil, nil
	}

	if item.Type != store.WorkTypeNew {
		return item, nil
	}

	worktreesEnabled, err := s.ConfigGetDefault("worktrees", "true")
	if err != nil || worktreesEnabled != "true" {
		return item, nil
	}

	ticket := item.Ticket
	repoPath := ticket.RepoPath
	if repoPath == "" {
		return nil, fmt.Errorf("claim: ticket %s has no repo_path set — re-import or re-draft with repo_path pointing to the git repo", ticket.ID)
	}

	featureBranch := ticket.FeatureBranch
	if featureBranch == "" {
		featureBranch = "feat/" + strings.ToLower(ticket.ID)
	}

	worktreeAbs := filepath.Join(repoPath, ".worktrees", ticket.ID)

	checkBranch := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", featureBranch)
	checkBranch.Stdout = io.Discard
	checkBranch.Stderr = io.Discard
	branchExists := checkBranch.Run() == nil

	var wtCmd *exec.Cmd
	if branchExists {
		wtCmd = exec.Command("git", "-C", repoPath, "worktree", "add", worktreeAbs, featureBranch)
	} else {
		wtCmd = exec.Command("git", "-C", repoPath, "worktree", "add", "-b", featureBranch, worktreeAbs)
	}
	var wtStderr bytes.Buffer
	wtCmd.Stdout = stdout
	wtCmd.Stderr = io.MultiWriter(stderr, &wtStderr)

	if err := wtCmd.Run(); err != nil {
		if msg := strings.TrimSpace(wtStderr.String()); msg != "" {
			return nil, fmt.Errorf("claim: worktree creation failed: %s", msg)
		}
		return nil, fmt.Errorf("claim: worktree creation failed: %w", err)
	}

	if err := s.SetWorktreePath(ticket.ID, worktreeAbs, repoPath, featureBranch); err != nil {
		return nil, fmt.Errorf("claim: could not save worktree_path: %w", err)
	}

	updated, err := s.GetTicket(ticket.ID)
	if err != nil {
		return nil, fmt.Errorf("claim: reload ticket: %w", err)
	}
	item.Ticket = updated
	return item, nil
}

// Ready transitions a ticket to ready. The worktree and feature branch are
// preserved across the review cycle; only Redraft and Merge tear them down.
// stdout and stderr are accepted for interface symmetry but are unused; pass
// io.Discard.
func Ready(s *store.Store, ticketID string, author string, stdout, stderr io.Writer) error {
	if err := s.TransitionTicket(ticketID, model.StatusReady, author); err != nil {
		return fmt.Errorf("ready: transition: %w", err)
	}
	return nil
}

// SubmitReview flushes all staged draft actions to the store atomically,
// auto-generates amendment tasks for each needs_attention thread, and
// transitions the ticket from in_review to ready only when at least one
// needs_attention thread exists. If all actions were resolutions, the ticket
// stays in_review. stdout and stderr are accepted for interface symmetry but
// are unused; pass io.Discard.
func SubmitReview(s *store.Store, ticketID string, author string, stdout, stderr io.Writer) error {
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

	if err := s.TransitionTicket(ticketID, model.StatusReady, author); err != nil {
		return fmt.Errorf("submit-review: transition ticket: %w", err)
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

// ReviewSubmit atomically flips all active threads to ready and transitions the
// ticket from in_review to ready. Returns an error if there are no active
// threads. stdout and stderr are accepted for interface symmetry but are unused;
// pass io.Discard.
func ReviewSubmit(s *store.Store, ticketID string, author string, stdout, stderr io.Writer) error {
	threads, err := s.GetThreadsForTicket(ticketID)
	if err != nil {
		return fmt.Errorf("review-submit: load threads: %w", err)
	}

	var openIDs []string
	for _, th := range threads {
		if th.Status == model.ThreadOpen {
			openIDs = append(openIDs, th.ID)
		}
	}
	if len(openIDs) == 0 {
		return fmt.Errorf("review-submit requires at least one open thread")
	}

	for _, id := range openIDs {
		if err := s.TransitionThread(id, model.ThreadNeedsAttention, author); err != nil {
			return fmt.Errorf("review-submit: transition thread %s: %w", id, err)
		}
	}

	if err := s.TransitionTicket(ticketID, model.StatusReady, author); err != nil {
		return fmt.Errorf("review-submit: transition ticket: %w", err)
	}
	return nil
}

// Redraft destroys the worktree and feature branch, clears the DB fields, and
// transitions the ticket back to draft. stdout and stderr control where git
// output goes; pass io.Discard to suppress.
func Redraft(s *store.Store, ticketID string, author string, stdout, stderr io.Writer) error {
	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		return fmt.Errorf("redraft: %w", err)
	}

	if ticket.WorktreePath != "" {
		repoPath := ticket.RepoPath
		wtCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", ticket.WorktreePath)
		wtCmd.Stdout = stdout
		wtCmd.Stderr = stderr
		if err := wtCmd.Run(); err != nil {
			fmt.Fprintf(stderr, "redraft: warning: could not remove worktree: %v\n", err)
		}

		if ticket.FeatureBranch != "" {
			delCmd := exec.Command("git", "-C", repoPath, "branch", "-D", ticket.FeatureBranch)
			delCmd.Stdout = stdout
			delCmd.Stderr = stderr
			if err := delCmd.Run(); err != nil {
				fmt.Fprintf(stderr, "redraft: warning: could not delete branch %s: %v\n", ticket.FeatureBranch, err)
			}
		}

		if err := s.SetWorktreePath(ticketID, "", "", ""); err != nil {
			return fmt.Errorf("redraft: clear worktree_path: %w", err)
		}
	}

	if err := s.TransitionTicket(ticketID, model.StatusDraft, author); err != nil {
		return fmt.Errorf("redraft: transition: %w", err)
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

	if ticket.Status != "approved" {
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

	if ticket.WorktreePath != "" {
		wtCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", ticket.WorktreePath)
		wtCmd.Stdout = stdout
		wtCmd.Stderr = stderr
		if err := wtCmd.Run(); err != nil {
			fmt.Fprintf(stderr, "merge: warning: could not remove worktree: %v\n", err)
		}
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

	if err := s.TransitionTicket(ticketID, "merged", "human"); err != nil {
		return fmt.Errorf("merge: transition: %w", err)
	}

	return nil
}
