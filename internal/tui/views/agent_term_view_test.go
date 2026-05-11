package views

import (
	"strings"
	"testing"
)


func TestAgentTermView_RunningState(t *testing.T) {
	v := NewAgentTermView("T-042")
	v.SetSize(20, 5)
	v.SetState(true, false)

	out := v.View()
	if !strings.Contains(out, ansiGreen) {
		t.Errorf("running state: expected green border, got:\n%s", out)
	}
	if strings.Contains(out, "[session ended]") || strings.Contains(out, "[crashed]") {
		t.Errorf("running state: unexpected suffix in label, got:\n%s", out)
	}
}

func TestAgentTermView_EndedState(t *testing.T) {
	v := NewAgentTermView("T-042")
	v.SetSize(40, 5)
	v.SetState(false, false)

	out := v.View()
	if !strings.Contains(out, ansiGray) {
		t.Errorf("ended state: expected gray border, got:\n%s", out)
	}
	if !strings.Contains(out, "[session ended]") {
		t.Errorf("ended state: expected '[session ended]' suffix, got:\n%s", out)
	}
}

func TestAgentTermView_CrashedState(t *testing.T) {
	v := NewAgentTermView("T-042")
	v.SetSize(40, 5)
	v.SetState(false, true)

	out := v.View()
	if !strings.Contains(out, ansiRed) {
		t.Errorf("crashed state: expected red border, got:\n%s", out)
	}
	if !strings.Contains(out, "[crashed]") {
		t.Errorf("crashed state: expected '[crashed]' suffix, got:\n%s", out)
	}
}

func TestAgentTermView_Resize(t *testing.T) {
	w, h := 30, 8
	v := NewAgentTermView("T-042")
	v.SetSize(w, h)
	v.SetState(true, false)

	out := v.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != h {
		t.Errorf("resize: expected %d lines, got %d", h, len(lines))
	}
	for i, line := range lines {
		runes := []rune(stripANSI(line))
		if len(runes) != w {
			t.Errorf("resize: line %d has %d runes, want %d: %q", i, len(runes), w, line)
		}
	}
}

func TestAgentTermView_LongLinesAreTruncated(t *testing.T) {
	w, h := 20, 5
	v := NewAgentTermView("T-042")
	v.SetSize(w, h)
	v.SetState(true, false)
	v.SetLines([]string{strings.Repeat("X", 100)})

	out := v.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// Content row 0 (line index 1) should be exactly w runes wide.
	for i, line := range lines {
		runes := []rune(stripANSI(line))
		if len(runes) != w {
			t.Errorf("truncation: line %d has %d runes, want %d: %q", i, len(runes), w, line)
		}
	}
}

func TestAgentTermView_ShortContent(t *testing.T) {
	w, h := 30, 8
	v := NewAgentTermView("T-042")
	v.SetSize(w, h)
	v.SetState(true, false)
	// Provide only one line of content, rest should be padded.
	v.SetLines([]string{"hello"})

	out := v.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != h {
		t.Errorf("short content: expected %d lines, got %d", h, len(lines))
	}
}

