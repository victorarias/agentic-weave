package tools

import (
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
)

func TestSanitizeLimitAndClampBytes(t *testing.T) {
	if got := sanitizeLimit(0, 10, 100); got != 10 {
		t.Fatalf("expected fallback 10, got %d", got)
	}
	if got := sanitizeLimit(500, 10, 100); got != 100 {
		t.Fatalf("expected hard max 100, got %d", got)
	}
	value, truncated := clampBytes("abcdef", 3)
	if value != "abc" || !truncated {
		t.Fatalf("expected truncated abc, got %q truncated=%v", value, truncated)
	}
}

func TestParseInputError(t *testing.T) {
	call := agentic.ToolCall{Name: "x", Input: json.RawMessage(`{"bad":`)}
	var target map[string]any
	res := parseInput(call, &target)
	if res == nil || res.Error == nil {
		t.Fatal("expected parseInput to return tool error")
	}
}
