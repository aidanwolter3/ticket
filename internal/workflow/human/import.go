package human

import (
	"fmt"

	"github.com/aidanwolter/ticket/internal/model"
)

// ImportTicket is one ticket entry in an import document.
type ImportTicket struct {
	Ref         string          `json:"ref"`
	Title       string          `json:"title"`
	Status      string          `json:"status"`
	Description string          `json:"description"`
	RepoPath    string          `json:"repo_path"`
	BlockedBy   []string        `json:"blocked_by"`
	Tasks       []ImportTask    `json:"tasks"`
	Notes       []ImportNote    `json:"notes"`
}

// ImportTask is one task entry within an ImportTicket.
type ImportTask struct {
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	VerifiableResult string          `json:"verifiable_result"`
	Threads          []ImportThread  `json:"threads"`
}

// ImportNote is a note entry within an ImportTicket.
type ImportNote struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

// ImportThread is a thread entry within an ImportTask.
type ImportThread struct {
	Messages []ImportMessage `json:"messages"`
}

// ImportMessage is a message within an ImportThread.
type ImportMessage struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

// ImportResult holds the ref → assigned ID mapping for created tickets.
type ImportResult struct {
	Created map[string]string `json:"created"`
}

// Import creates tickets (with tasks, notes, and threads) from a batch import
// document.  Ref fields are resolved within the document; unknown refs are
// treated as existing ticket IDs.
func (w *Workflow) Import(inputs []ImportTicket) (*ImportResult, error) {
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
			Title:       in.Title,
			Type:        model.TypeTicket,
			Status:      status,
			Description: in.Description,
			RepoPath:    in.RepoPath,
		}
		if err := w.s.CreateTicket(t); err != nil {
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
		if err := w.s.UpdateTicket(created[i]); err != nil {
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
			if err := w.s.CreateTask(task); err != nil {
				return nil, fmt.Errorf("create task %q for %s: %w", it.Title, ticketID, err)
			}
			for _, th := range it.Threads {
				thread, err := w.s.CreateThread(task.ID, "", "")
				if err != nil {
					return nil, fmt.Errorf("create thread on task %s: %w", task.ID, err)
				}
				for _, msg := range th.Messages {
					if _, err := w.s.AddMessage(thread.ID, msg.Author, msg.Text); err != nil {
						return nil, fmt.Errorf("add message to thread %s: %w", thread.ID, err)
					}
				}
			}
		}

		for _, n := range in.Notes {
			if _, err := w.s.AddNote(ticketID, n.Author, n.Text); err != nil {
				return nil, fmt.Errorf("add note to %s: %w", ticketID, err)
			}
		}
	}

	resultMap := make(map[string]string, len(inputs))
	for k, v := range refToID {
		resultMap[k] = v
	}
	return &ImportResult{Created: resultMap}, nil
}
