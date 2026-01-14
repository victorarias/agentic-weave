package executor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
)

type stubExecutor struct {
	defs    []agentic.ToolDefinition
	results map[string]agentic.ToolResult
}

func (s stubExecutor) ListTools(ctx context.Context) ([]agentic.ToolDefinition, error) {
	return s.defs, nil
}

func (s stubExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if result, ok := s.results[call.Name]; ok {
		return result, nil
	}
	return agentic.ToolResult{}, agentic.ErrToolNotFound
}

func TestCompositeListTools(t *testing.T) {
	c := NewComposite(
		stubExecutor{defs: []agentic.ToolDefinition{{Name: "a"}}},
		stubExecutor{defs: []agentic.ToolDefinition{{Name: "b"}}},
	)
	defs, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools")
	}
}

func TestFilteredExecutor(t *testing.T) {
	inner := stubExecutor{defs: []agentic.ToolDefinition{{Name: "a"}, {Name: "b"}}}
	f := NewFiltered(inner, []string{"a"})
	defs, err := f.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "a" {
		t.Fatalf("expected only tool a")
	}
}

func TestParallelExecutor(t *testing.T) {
	payload := json.RawMessage(`{"ok":true}`)
	inner := stubExecutor{results: map[string]agentic.ToolResult{
		"a": {Name: "a", Output: payload},
		"b": {Name: "b", Output: payload},
	}}
	p := NewParallel(inner, []string{"a", "b"})
	results, err := p.ExecuteBatch(context.Background(), []agentic.ToolCall{{Name: "a"}, {Name: "b"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results")
	}
}
