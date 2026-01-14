package mcp

import (
	"context"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
)

type stubClient struct {
	defs   []agentic.ToolDefinition
	result agentic.ToolResult
}

func (s stubClient) ListTools(ctx context.Context) ([]agentic.ToolDefinition, error) {
	return s.defs, nil
}

func (s stubClient) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return s.result, nil
}

func TestRegistryAllowlist(t *testing.T) {
	client := stubClient{defs: []agentic.ToolDefinition{{Name: "a"}, {Name: "b"}}}
	reg := NewRegistry(client, []string{"b"})

	defs, err := reg.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "b" {
		t.Fatalf("expected allowlisted tool")
	}
}

func TestRegistryNilClient(t *testing.T) {
	reg := NewRegistry(nil, nil)
	defs, err := reg.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected no tools")
	}
	_, err = reg.Execute(context.Background(), agentic.ToolCall{Name: "a"})
	if err != agentic.ErrToolNotFound {
		t.Fatalf("expected tool not found")
	}
}
