package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runPromote(args []string, defaultDB string) {
	fs := flag.NewFlagSet("promote", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket promote [--db path] <ticket-id> <author>")
		fmt.Fprintln(os.Stderr, "  author: human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)

	s := openStore(*dbPath)
	defer s.Close()

	ticket, err := s.PromoteTicket(ticketID, author)
	if err != nil {
		fmt.Fprintf(os.Stderr, "promote failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → ready\n", ticketID)

	// Check whether worktrees are enabled.
	worktreesEnabled, err := s.ConfigGetDefault("worktrees", "true")
	if err != nil || worktreesEnabled != "true" {
		return
	}

	// Determine repo_path.
	repoPath := ticket.RepoPath
	if repoPath == "" {
		fmt.Fprintf(os.Stderr, "promote: ticket %s has no repo_path set — re-import or re-draft with repo_path pointing to the git repo\n", ticketID)
		os.Exit(1)
	}

	// Determine feature branch.
	featureBranch := ticket.FeatureBranch
	if featureBranch == "" {
		featureBranch = "feat/" + strings.ToLower(ticketID)
	}

	// Create the worktree.
	worktreeRel := filepath.Join(".worktrees", ticketID)
	worktreeAbs := filepath.Join(repoPath, worktreeRel)

	// Check whether the branch exists (suppress output — non-zero exit just means it doesn't).
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
		fmt.Fprintf(os.Stderr, "warning: worktree creation failed: %v\n", err)
		return
	}

	if err := s.SetWorktreePath(ticketID, worktreeAbs, repoPath, featureBranch); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save worktree_path: %v\n", err)
		return
	}

	fmt.Printf("worktree: %s (branch: %s)\n", worktreeAbs, featureBranch)
}
