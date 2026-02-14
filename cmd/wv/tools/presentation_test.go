package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
)

func TestSummarizeToolUpdateSanitizesControlBytes(t *testing.T) {
	out, _ := json.Marshal(map[string]any{
		"stdout": "\x1b[31mhello\x1b[0m",
		"stderr": "",
	})
	result := &agentic.ToolResult{Name: "bash", Output: out}
	summary, details, _ := SummarizeToolUpdate(result)
	if strings.Contains(summary, "\x1b") || strings.Contains(details, "\x1b") {
		t.Fatalf("expected sanitized summary/details, got summary=%q details=%q", summary, details)
	}
}

func TestSummarizeToolUpdateSanitizesWritePath(t *testing.T) {
	out, _ := json.Marshal(map[string]any{
		"path": "a\x1b[31mb.txt",
	})
	result := &agentic.ToolResult{Name: "write", Output: out}
	summary, _, _ := SummarizeToolUpdate(result)
	if strings.Contains(summary, "\x1b") {
		t.Fatalf("expected sanitized path summary, got %q", summary)
	}
	if summary != "a[31mb.txt" {
		t.Fatalf("unexpected sanitized summary: %q", summary)
	}
}
