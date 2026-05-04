package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

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
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, t := range tickets {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.ID, t.Title, t.Type, t.Status)
	}
	w.Flush()
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
