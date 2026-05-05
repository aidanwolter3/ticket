package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func runMerge(args []string, defaultDB string) {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket merge [--db path] <id> <author>")
		fmt.Fprintln(os.Stderr, "  author must be human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)

	if author != "human" && !strings.HasPrefix(author, "human:") {
		fmt.Fprintf(os.Stderr, "merge: only humans may merge tickets (got %q)\n", author)
		os.Exit(1)
	}

	s := openStore(*dbPath)
	defer s.Close()

	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "merge: %v\n", err)
		os.Exit(1)
	}

	// Precondition: ticket must be approved.
	if ticket.Status != "approved" {
		fmt.Fprintf(os.Stderr, "merge: ticket %s is %s, not approved\n", ticketID, ticket.Status)
		os.Exit(1)
	}

	// Precondition: all tasks must be complete.
	for _, task := range ticket.Tasks {
		if task.CompletedAt == nil {
			fmt.Fprintf(os.Stderr, "merge: task %s (%q) is not complete\n", task.ID, task.Title)
			os.Exit(1)
		}
	}

	// Precondition: no open threads.
	for _, task := range ticket.Tasks {
		threads, err := s.GetThreadsForTask(task.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "merge: load threads: %v\n", err)
			os.Exit(1)
		}
		for _, th := range threads {
			if th.Status == "active" || th.Status == "ready" {
				fmt.Fprintf(os.Stderr, "merge: ticket %s has open thread %s — resolve all threads before merging\n",
					ticketID, th.ID)
				os.Exit(1)
			}
		}
	}

	// Precondition: repo_path must be set.
	repoPath := ticket.RepoPath
	if repoPath == "" {
		fmt.Fprintf(os.Stderr, "merge: ticket %s has no repo_path set\n", ticketID)
		os.Exit(1)
	}
	if _, err := os.Stat(repoPath); err != nil {
		fmt.Fprintf(os.Stderr, "merge: repo_path %q does not exist: %v\n", repoPath, err)
		os.Exit(1)
	}

	// Precondition: feature_branch must be set.
	if ticket.FeatureBranch == "" {
		fmt.Fprintf(os.Stderr, "merge: ticket %s has no feature_branch set\n", ticketID)
		os.Exit(1)
	}
	featureBranch := ticket.FeatureBranch

	// Git: fast-forward merge.
	mergeCmd := exec.Command("git", "-C", repoPath, "merge", "--ff-only", featureBranch)
	mergeCmd.Stdout = os.Stdout
	mergeCmd.Stderr = os.Stderr
	if err := mergeCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr,
			"merge: branch has diverged from main — rebase manually then retry `ticket merge`\n")
		os.Exit(1)
	}

	// Git: remove worktree before deleting the branch (branch may be checked out there).
	if ticket.WorktreePath != "" {
		wtCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", ticket.WorktreePath)
		wtCmd.Stdout = os.Stdout
		wtCmd.Stderr = os.Stderr
		if err := wtCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "merge: warning: could not remove worktree: %v\n", err)
		}
	}

	// Git: delete feature branch.
	delCmd := exec.Command("git", "-C", repoPath, "branch", "-d", featureBranch)
	delCmd.Stdout = os.Stdout
	delCmd.Stderr = os.Stderr
	if err := delCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "merge: warning: could not delete branch %s: %v\n", featureBranch, err)
	}

	// DB: clear worktree_path and feature_branch.
	ticket.WorktreePath = ""
	ticket.FeatureBranch = ""
	if err := s.UpdateTicket(ticket); err != nil {
		fmt.Fprintf(os.Stderr, "merge: update ticket: %v\n", err)
		os.Exit(1)
	}
	// Transition to merged.
	if err := s.TransitionTicket(ticketID, "merged", author); err != nil {
		fmt.Fprintf(os.Stderr, "merge: transition: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → merged\n", ticketID)
}
