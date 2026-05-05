package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/aidanwolter/ticket/internal/model"
	"github.com/aidanwolter/ticket/internal/store"
)

// importInput is the top-level JSON structure agents POST to create tickets.
type importInput struct {
	Tickets []importTicket `json:"tickets"`
}

type importTicket struct {
	// Ref is a local name used only within this JSON document to wire up
	// blocked_by relationships between tickets being created together.
	// It is not stored in the database.
	Ref           string        `json:"ref"`
	Title         string        `json:"title"`
	Status        string        `json:"status"` // defaults to "draft"
	Description   string        `json:"description"`
	FeatureBranch string        `json:"feature_branch"`
	WorktreePath  string        `json:"worktree_path"`
	RepoPath      string        `json:"repo_path"`
	// BlockedBy may contain refs from this document ("jwt") or existing IDs ("T-042").
	BlockedBy []string       `json:"blocked_by"`
	Tasks     []importTask   `json:"tasks"`
	Notes     []importNote   `json:"notes"`
	Threads   []importThread `json:"threads"` // deprecated: threads now belong to tasks
}

type importTask struct {
	Title            string         `json:"title"`
	Description      string         `json:"description"`
	VerifiableResult string         `json:"verifiable_result"`
	Threads          []importThread `json:"threads"`
}

type importNote struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

type importThread struct {
	Messages []importMessage `json:"messages"`
}

type importMessage struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

type importResult struct {
	Created map[string]string `json:"created"` // ref → assigned ID (or title → ID if no ref)
}

func runImport(args []string, defaultDB string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	fs.Parse(args)

	var r io.Reader = os.Stdin
	if fs.NArg() > 0 {
		f, err := os.Open(fs.Arg(0))
		if err != nil {
			fmt.Fprintf(os.Stderr, "open file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		r = f
	}

	var input importInput
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		fmt.Fprintf(os.Stderr, "decode JSON: %v\n", err)
		os.Exit(1)
	}

	if len(input.Tickets) == 0 {
		fmt.Fprintln(os.Stderr, "no tickets in input")
		os.Exit(1)
	}

	s := openStore(*dbPath)
	defer s.Close()

	result, err := importTickets(s, input.Tickets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(result)
}

func importTickets(s *store.Store, inputs []importTicket) (*importResult, error) {
	// First pass: create all tickets without blocked_by so we can resolve refs.
	refToID := make(map[string]string, len(inputs))
	created := make([]*model.Ticket, 0, len(inputs))

	for _, in := range inputs {
		if in.Title == "" {
			return nil, fmt.Errorf("ticket with ref %q is missing a title", in.Ref)
		}

		status := model.StatusDraft
		if in.Status != "" {
			status = model.Status(in.Status)
		}

		t := &model.Ticket{
			Title:         in.Title,
			Type:          model.TypeTicket,
			Status:        status,
			Description:   in.Description,
			FeatureBranch: in.FeatureBranch,
			WorktreePath:  in.WorktreePath,
			RepoPath:      in.RepoPath,
		}
		if err := s.CreateTicket(t); err != nil {
			return nil, fmt.Errorf("create ticket %q: %w", in.Title, err)
		}

		key := in.Ref
		if key == "" {
			key = in.Title
		}
		refToID[key] = t.ID
		created = append(created, t)
	}

	// Second pass: resolve blocked_by refs and update tickets that have them.
	for i, in := range inputs {
		if len(in.BlockedBy) == 0 {
			continue
		}
		resolved := make([]string, 0, len(in.BlockedBy))
		for _, ref := range in.BlockedBy {
			if id, ok := refToID[ref]; ok {
				resolved = append(resolved, id)
			} else {
				resolved = append(resolved, ref)
			}
		}
		created[i].BlockedBy = resolved
		if err := s.UpdateTicket(created[i]); err != nil {
			return nil, fmt.Errorf("set blocked_by for %s: %w", created[i].ID, err)
		}
	}

	// Third pass: create tasks, notes, and threads.
	for i, in := range inputs {
		ticketID := created[i].ID

		for pos, it := range in.Tasks {
			task := &model.Task{
				TicketID:         ticketID,
				Title:            it.Title,
				Description:      it.Description,
				Position:         pos + 1,
				VerifiableResult: it.VerifiableResult,
			}
			if err := s.CreateTask(task); err != nil {
				return nil, fmt.Errorf("create task %q for %s: %w", it.Title, ticketID, err)
			}
			for _, th := range it.Threads {
				thread, err := s.CreateThread(task.ID)
				if err != nil {
					return nil, fmt.Errorf("create thread on task %s: %w", task.ID, err)
				}
				for _, msg := range th.Messages {
					if _, err := s.AddMessage(thread.ID, msg.Author, msg.Text); err != nil {
						return nil, fmt.Errorf("add message to thread %s: %w", thread.ID, err)
					}
				}
			}
		}

		for _, n := range in.Notes {
			if _, err := s.AddNote(ticketID, n.Author, n.Text); err != nil {
				return nil, fmt.Errorf("add note to %s: %w", ticketID, err)
			}
		}
	}

	resultMap := make(map[string]string, len(inputs))
	for k, v := range refToID {
		resultMap[k] = v
	}
	return &importResult{Created: resultMap}, nil
}
