package views

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aidanwolter/ticket/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Force ANSI output so lipgloss styling is testable without a TTY.
	lipgloss.DefaultRenderer().SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

// stripANSI removes ANSI escape sequences so we can count visible runes.
func stripANSI(s string) string {
	var out strings.Builder
	i := 0
	runes := []rune(s)
	for i < len(runes) {
		if runes[i] == '\033' && i+1 < len(runes) && runes[i+1] == '[' {
			i += 2
			for i < len(runes) && runes[i] != 'm' {
				i++
			}
			i++ // skip 'm'
			continue
		}
		out.WriteRune(runes[i])
		i++
	}
	return out.String()
}
