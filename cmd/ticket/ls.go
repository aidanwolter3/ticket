package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	xterm "github.com/charmbracelet/x/term"

	"github.com/aidanwolter/ticket/internal/model"
)

// ticketJSON is the JSON representation of a ticket for CLI output.
type ticketJSON struct {
	ID               string       `json:"id"`
	Title            string       `json:"title"`
	Type             string       `json:"type"`
	Status           string       `json:"status"`
	Description      string       `json:"description,omitempty"`
	FeatureBranch    string       `json:"feature_branch,omitempty"`
	StackID          string       `json:"stack_id,omitempty"`
	CommitHash       string       `json:"commit_hash,omitempty"`
	VerifiableResult string       `json:"verifiable_result,omitempty"`
	BlockedBy        []string     `json:"blocked_by,omitempty"`
	Threads          []threadJSON `json:"threads,omitempty"`
	Notes            []noteJSON   `json:"notes,omitempty"`
	Created          time.Time    `json:"created"`
	Updated          time.Time    `json:"updated"`
}

type threadJSON struct {
	ID       string        `json:"id"`
	Status   string        `json:"status"`
	Messages []messageJSON `json:"messages,omitempty"`
	Created  time.Time     `json:"created"`
}

type messageJSON struct {
	ID      string    `json:"id"`
	Author  string    `json:"author"`
	Text    string    `json:"text"`
	Created time.Time `json:"created"`
}

type noteJSON struct {
	ID      string    `json:"id"`
	Author  string    `json:"author"`
	Text    string    `json:"text"`
	Created time.Time `json:"created"`
}

func runList(args []string, defaultDB string) {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	statusFilter := fs.String("status", "", "filter by status (draft|ready|in_progress|in_review|completed)")
	jsonOut := fs.Bool("json", false, "output full ticket data as JSON")
	fs.Parse(args)

	s := openStore(*dbPath)
	defer s.Close()

	var tickets []*model.Ticket
	var err error
	if *statusFilter != "" {
		tickets, err = s.ListTickets(model.Status(*statusFilter))
	} else {
		tickets, err = s.ListTickets()
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
	maxIDLen, maxTypeLen, maxStatusLen := 0, 0, 0
	for _, t := range tickets {
		if l := len(t.ID); l > maxIDLen {
			maxIDLen = l
		}
		if !t.IsPlan() {
			// blank for regular tickets
		} else if l := len(string(t.Type)); l > maxTypeLen {
			maxTypeLen = l
		}
		if l := len(string(t.Status)); l > maxStatusLen {
			maxStatusLen = l
		}
	}
	// 3 padding groups of 2 spaces (between 4 columns)
	maxTitleLen := termW - maxIDLen - maxTypeLen - maxStatusLen - 6
	if maxTitleLen < 10 {
		maxTitleLen = 10
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, t := range tickets {
		typeStr := ""
		if t.IsPlan() {
			typeStr = string(t.Type)
		}
		title := truncateRunes(t.Title, maxTitleLen)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.ID, title, typeStr, t.Status)
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

func toTicketJSON(t *model.Ticket) ticketJSON {
	tj := ticketJSON{
		ID:               t.ID,
		Title:            t.Title,
		Type:             string(t.Type),
		Status:           string(t.Status),
		Description:      t.Description,
		FeatureBranch:    t.FeatureBranch,
		StackID:          t.StackID,
		CommitHash:       t.CommitHash,
		VerifiableResult: t.VerifiableResult,
		BlockedBy:        t.BlockedBy,
		Created:          t.Created,
		Updated:          t.Updated,
	}
	for _, th := range t.Threads {
		thj := threadJSON{
			ID:      th.ID,
			Status:  string(th.Status),
			Created: th.Created,
		}
		for _, m := range th.Messages {
			thj.Messages = append(thj.Messages, messageJSON{
				ID:      m.ID,
				Author:  m.Author,
				Text:    m.Text,
				Created: m.Created,
			})
		}
		tj.Threads = append(tj.Threads, thj)
	}
	for _, n := range t.Notes {
		tj.Notes = append(tj.Notes, noteJSON{
			ID:      n.ID,
			Author:  n.Author,
			Text:    n.Text,
			Created: n.Created,
		})
	}
	return tj
}
