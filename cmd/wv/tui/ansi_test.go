package tui

import (
	"strings"
	"testing"
)

func TestWrapTextPreservesRepeatedWhitespace(t *testing.T) {
	lines := WrapText("a  b\tc", 5)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped lines, got %#v", lines)
	}
	if lines[0] != "a  b\t" {
		t.Fatalf("expected whitespace-preserving first line, got %q", lines[0])
	}
	if lines[1] != "c" {
		t.Fatalf("expected second line to carry remaining text, got %q", lines[1])
	}
}

func TestTruncateVisiblePreservesANSIReset(t *testing.T) {
	input := "\x1b[32mhello world\x1b[0m"
	out := TruncateVisible(input, 8)
	if !strings.HasSuffix(out, "\x1b[0m") {
		t.Fatalf("expected reset suffix, got %q", out)
	}
	if VisibleWidth(out) != 8 {
		t.Fatalf("expected visible width 8, got %d (%q)", VisibleWidth(out), out)
	}
}
