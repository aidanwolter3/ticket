package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/aidanwolter/ticket/internal/workflow/human"
)

// importInput is the top-level JSON structure for importing tickets.
type importInput struct {
	Tickets []human.ImportTicket `json:"tickets"`
}

func RunImport(args []string, wf *human.Workflow) {
	var r io.Reader = os.Stdin
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		f, err := os.Open(args[0])
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

	result, err := wf.Import(input.Tickets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(result)
}
