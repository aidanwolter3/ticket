package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/aidanwolter/ticket/internal/model"
)

func RunApprove(args []string, defaultDB string) {
	s, fs := parseAndOpen("approve", args, defaultDB, nil)
	defer s.Close()

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: ticket approve [--db path] <id> <author>")
		fmt.Fprintln(os.Stderr, "  author must be human:<name>")
		os.Exit(1)
	}
	ticketID := fs.Arg(0)
	author := fs.Arg(1)

	if author != "human" && !strings.HasPrefix(author, "human:") {
		fmt.Fprintf(os.Stderr, "approve: only humans may approve tickets (got %q)\n", author)
		os.Exit(1)
	}

	ticket, err := s.GetTicket(ticketID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "approve: %v\n", err)
		os.Exit(1)
	}

	if ticket.Status != model.StatusInReview {
		fmt.Fprintf(os.Stderr, "approve: ticket %s is %s, not in_review\n", ticketID, ticket.Status)
		os.Exit(1)
	}

	// Check for open threads.
	for _, task := range ticket.Tasks {
		threads, err := s.GetThreadsForTask(task.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "approve: load threads: %v\n", err)
			os.Exit(1)
		}
		for _, th := range threads {
			if th.Status == model.ThreadOpen || th.Status == model.ThreadNeedsAttention {
				fmt.Fprintf(os.Stderr, "approve: ticket %s has open thread %s (status: %s) — resolve all threads before approving\n",
					ticketID, th.ID, th.Status)
				os.Exit(1)
			}
		}
	}

	if err := s.TransitionTicket(ticketID, model.StatusApproved, author); err != nil {
		fmt.Fprintf(os.Stderr, "approve: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s → approved\n", ticketID)
}
