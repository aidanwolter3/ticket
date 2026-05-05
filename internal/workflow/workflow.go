package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aidanwolter/ticket/internal/store"
)

// Promote transitions a draft ticket to ready and creates a git worktree for it
// if worktrees are enabled in config.
func Promote(s *store.Store, ticketID string) error {
	ticket, err := s.PromoteTicket(ticketID, "human")
	if err != nil {
		return fmt.Errorf("promote: %w", err)
	}

	worktreesEnabled, err := s.ConfigGetDefault("worktrees", "true")
	if err != nil || worktreesEnabled != "true" {
		return nil
	}

	repoPath := ticket.RepoPath
	if repoPath == "" {
		return fmt.Errorf("promote: ticket %s has no repo_path set — re-import or re-draft with repo_path pointing to the git repo", ticketID)
	}

	featureBranch := ticket.FeatureBranch
	if featureBranch == "" {
		featureBranch = "feat/" + strings.ToLower(ticketID)
	}

	worktreeAbs := filepath.Join(repoPath, ".worktrees", ticketID)

	checkBranch := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", featureBranch)
	branchExists := checkBranch.Run() == nil

	var wtCmd *exec.Cmd
	if branchExists {
		wtCmd = exec.Command("git", "-C", repoPath, "worktree", "add", worktreeAbs, featureBranch)
	} else {
		wtCmd = exec.Command("git", "-C", repoPath, "worktree", "add", "-b", featureBranch, worktreeAbs)
	}
	wtCmd.Stdout = os.Stderr
	wtCmd.Stderr = os.Stderr

	if err := wtCmd.Run(); err != nil {
		return fmt.Errorf("promote: worktree creation failed: %w", err)
	}

	if err := s.SetWorktreePath(ticketID, worktreeAbs, repoPath, featureBranch); err != nil {
		return fmt.Errorf("promote: could not save worktree_path: %w", err)
	}

	return nil
}

// Merge ff-merges the feature branch into main, removes the worktree, deletes
// the branch, and transitions the ticket to merged.
func Merge(s *store.Store, ticketID string) error {
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
			if th.Status == "active" || th.Status == "ready" {
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

	mergeCmd := exec.Command("git", "-C", repoPath, "merge", "--ff-only", featureBranch)
	mergeCmd.Stdout = os.Stdout
	mergeCmd.Stderr = os.Stderr
	if err := mergeCmd.Run(); err != nil {
		return fmt.Errorf("merge: branch has diverged from main — rebase manually then retry")
	}

	if ticket.WorktreePath != "" {
		wtCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", ticket.WorktreePath)
		wtCmd.Stdout = os.Stdout
		wtCmd.Stderr = os.Stderr
		if err := wtCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "merge: warning: could not remove worktree: %v\n", err)
		}
	}

	delCmd := exec.Command("git", "-C", repoPath, "branch", "-d", featureBranch)
	delCmd.Stdout = os.Stdout
	delCmd.Stderr = os.Stderr
	if err := delCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "merge: warning: could not delete branch %s: %v\n", featureBranch, err)
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
