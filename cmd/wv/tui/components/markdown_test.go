package components

import (
	"strings"
	"testing"
)

func TestMarkdownRenderPlainAndCodeBlock(t *testing.T) {
	m := NewMarkdown("line one\n\n```go\nfmt.Println(\"hi\")\n```")
	lines := m.Render(40)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "line one") {
		t.Fatalf("expected plain markdown content, got %q", joined)
	}
	if !strings.Contains(joined, "Println") {
		t.Fatalf("expected code block output, got %q", joined)
	}
}

func TestMarkdownRenderFallbackForInvalidLanguage(t *testing.T) {
	lines := renderCodeBlock("x", "definitely-not-a-lang")
	if len(lines) == 0 {
		t.Fatal("expected fallback output for invalid lexer")
	}
}
