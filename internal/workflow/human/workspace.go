package human

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aidanwolter/ticket/internal/store"
)

// Workspace manages the lifecycle of a ticket's workspace.
type Workspace interface {
	Create(ticketID string, stdout, stderr io.Writer) (path string, err error)
	Delete(ticketID string, stdout, stderr io.Writer) error
}

// WorktreeWorkspace implements Workspace using git worktrees.
type WorktreeWorkspace struct {
	s *store.Store
}

// Create creates a git worktree for the ticket and persists the path. If a
// worktree path is already set, it returns it without creating a new one.
func (w WorktreeWorkspace) Create(ticketID string, stdout, stderr io.Writer) (string, error) {
	ticket, err := w.s.GetTicket(ticketID)
	if err != nil {
		return "", err
	}

	if ticket.WorktreePath != "" {
		return ticket.WorktreePath, nil
	}

	if ticket.RepoPath == "" {
		return "", nil
	}

	featureBranch := ticket.FeatureBranch
	if featureBranch == "" {
		featureBranch = "feat/" + strings.ToLower(ticketID)
	}
	worktreeAbs := filepath.Join(ticket.RepoPath, ".worktrees", ticketID)

	checkBranch := exec.Command("git", "-C", ticket.RepoPath, "rev-parse", "--verify", featureBranch)
	checkBranch.Stdout = io.Discard
	checkBranch.Stderr = io.Discard
	branchExists := checkBranch.Run() == nil

	var wtCmd *exec.Cmd
	if branchExists {
		wtCmd = exec.Command("git", "-C", ticket.RepoPath, "worktree", "add", worktreeAbs, featureBranch)
	} else {
		wtCmd = exec.Command("git", "-C", ticket.RepoPath, "worktree", "add", "-b", featureBranch, worktreeAbs)
	}
	var wtStderr bytes.Buffer
	wtCmd.Stdout = stdout
	wtCmd.Stderr = io.MultiWriter(stderr, &wtStderr)
	if err := wtCmd.Run(); err != nil {
		msg := strings.TrimSpace(wtStderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("worktree creation failed: %s", msg)
	}

	if err := w.s.SetWorktreePath(ticketID, worktreeAbs, ticket.RepoPath, featureBranch); err != nil {
		exec.Command("git", "-C", ticket.RepoPath, "worktree", "remove", "--force", worktreeAbs).Run() //nolint:errcheck
		return "", fmt.Errorf("save worktree_path: %w", err)
	}

	return worktreeAbs, nil
}

// Delete removes the git worktree and clears the workspace path from the DB.
// If the worktree removal fails, a warning is printed but the error is not
// returned; only a ClearWorktree failure is fatal.
func (w WorktreeWorkspace) Delete(ticketID string, stdout, stderr io.Writer) error {
	ticket, err := w.s.GetTicket(ticketID)
	if err != nil {
		return err
	}

	if ticket.WorktreePath != "" {
		wtCmd := exec.Command("git", "-C", ticket.RepoPath, "worktree", "remove", "--force", ticket.WorktreePath)
		wtCmd.Stdout = stdout
		wtCmd.Stderr = stderr
		if err := wtCmd.Run(); err != nil {
			fmt.Fprintf(stderr, "worktree remove: warning: %v\n", err)
		}
	}

	return w.s.ClearWorktree(ticketID)
}
