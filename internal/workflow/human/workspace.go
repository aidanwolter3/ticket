package human

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
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

// CommandWorkspace implements Workspace using user-configured shell commands.
type CommandWorkspace struct {
	s *store.Store
}

// Create runs workspace.create_command with TICKET_ID set and persists the last
// non-empty stdout line as the workspace path.
func (w CommandWorkspace) Create(ticketID string, stdout, stderr io.Writer) (string, error) {
	ticket, err := w.s.GetTicket(ticketID)
	if err != nil {
		return "", err
	}

	if ticket.WorktreePath != "" {
		return ticket.WorktreePath, nil
	}

	createCmd, _, err := w.s.ConfigGet("workspace.create_command")
	if err != nil {
		return "", fmt.Errorf("read workspace.create_command: %w", err)
	}

	logPath := filepath.Join(os.TempDir(), "ticket-workspace-"+ticketID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	var capturedStdout bytes.Buffer
	cmd := exec.Command("sh", "-c", createCmd)
	cmd.Env = append(os.Environ(), "TICKET_ID="+ticketID)
	cmd.Stdout = io.MultiWriter(&capturedStdout, logFile, stdout)
	cmd.Stderr = io.MultiWriter(logFile, stderr)

	runErr := cmd.Run()

	wsPath := lastNonEmptyLine(capturedStdout.String())

	if runErr != nil {
		return "", fmt.Errorf("workspace create command failed: %w", runErr)
	}

	if err := w.s.SetWorktreePath(ticketID, wsPath, ticket.RepoPath, ""); err != nil {
		return "", fmt.Errorf("save workspace path: %w", err)
	}

	return wsPath, nil
}

// Delete runs workspace.delete_command with TICKET_ID set.
func (w CommandWorkspace) Delete(ticketID string, stdout, stderr io.Writer) error {
	deleteCmd, _, err := w.s.ConfigGet("workspace.delete_command")
	if err != nil {
		return fmt.Errorf("read workspace.delete_command: %w", err)
	}

	logPath := filepath.Join(os.TempDir(), "ticket-workspace-"+ticketID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command("sh", "-c", deleteCmd)
	cmd.Env = append(os.Environ(), "TICKET_ID="+ticketID)
	cmd.Stdout = io.MultiWriter(logFile, stdout)
	cmd.Stderr = io.MultiWriter(logFile, stderr)

	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("workspace delete command failed: %w", runErr)
	}
	return nil
}

// lastNonEmptyLine returns the last non-empty line from s.
func lastNonEmptyLine(s string) string {
	sc := bufio.NewScanner(strings.NewReader(s))
	var last string
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			last = line
		}
	}
	return last
}

// NewWorkspace returns the appropriate Workspace implementation based on the
// workspace.type config key. Validates config before returning.
func NewWorkspace(s *store.Store) (Workspace, error) {
	wsType, _, err := s.ConfigGet("workspace.type")
	if err != nil {
		return nil, fmt.Errorf("read workspace.type: %w", err)
	}
	if wsType == "" {
		wsType = "worktree"
	}

	createCmd, hasCreate, _ := s.ConfigGet("workspace.create_command")
	deleteCmd, hasDelete, _ := s.ConfigGet("workspace.delete_command")

	if wsType == "worktree" {
		if hasCreate && createCmd != "" {
			return nil, fmt.Errorf("workspace.type=worktree but workspace.create_command is set")
		}
		if hasDelete && deleteCmd != "" {
			return nil, fmt.Errorf("workspace.type=worktree but workspace.delete_command is set")
		}
		return WorktreeWorkspace{s: s}, nil
	}

	if !hasDelete || deleteCmd == "" {
		return nil, fmt.Errorf("workspace.type=%q requires workspace.delete_command to be set", wsType)
	}

	return CommandWorkspace{s: s}, nil
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
