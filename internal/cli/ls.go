package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	xterm "github.com/charmbracelet/x/term"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/workflow/human"
)

func RunList(args []string, wf *human.Workflow) {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	statusFilter := fs.String("status", "", "filter by status (draft|ready|in_progress|in_review|completed)")
	jsonOut := fs.Bool("json", false, "output full ticket data as JSON")
	showBacklog := fs.Bool("backlog", false, "show backlogged tickets instead of normal tickets")
	fs.Parse(args)

	var tickets []*model.Ticket
	var err error
	if *showBacklog {
		tickets, err = wf.ListBacklogTickets()
	} else if *statusFilter != "" {
		tickets, err = wf.ListTickets(model.Status(*statusFilter))
	} else {
		tickets, err = wf.ListTickets()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "list tickets: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		out := make([]ticketJSON, 0, len(tickets))
		for _, t := range tickets {
			out = append(out, toTicketJSON(t))
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	if len(tickets) == 0 {
		fmt.Println("no tickets")
		return
	}

	termW := termWidth()
	maxIDLen, maxStatusLen := 0, 0
	for _, t := range tickets {
		if l := len(t.ID); l > maxIDLen {
			maxIDLen = l
		}
		if l := len(string(t.Status)); l > maxStatusLen {
			maxStatusLen = l
		}
	}
	maxTitleLen := termW - maxIDLen - maxStatusLen - 4
	if maxTitleLen < 10 {
		maxTitleLen = 10
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, t := range tickets {
		title := truncateRunes(t.Title, maxTitleLen)
		fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, title, t.Status)
	}
	w.Flush()
}

func termWidth() int {
	w, _, err := xterm.GetSize(os.Stdout.Fd())
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
