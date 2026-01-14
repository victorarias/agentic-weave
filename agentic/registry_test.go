package agentic

import (
	"context"
	"encoding/json"
	"testing"
)

type echoTool struct {
	def ToolDefinition
}

func (e echoTool) Definition() ToolDefinition { return e.def }

func (e echoTool) Execute(ctx context.Context, call ToolCall) (ToolResult, error) {
	return ToolResult{ID: call.ID, Name: call.Name, Output: call.Input}, nil
}

func TestRegistrySchemaMismatch(t *testing.T) {
	reg := NewRegistry()
	reg.Register(echoTool{def: ToolDefinition{Name: "echo", SchemaHash: "abc"}})

	_, err := reg.Execute(context.Background(), ToolCall{Name: "echo", SchemaHash: "def"})
	if err != ErrSchemaMismatch {
		t.Fatalf("expected schema mismatch, got %v", err)
	}
}

func TestRegistryCallerGating(t *testing.T) {
	reg := NewRegistry()
	reg.Register(echoTool{def: ToolDefinition{Name: "echo", AllowedCallers: []string{"programmatic"}}})

	_, err := reg.Execute(context.Background(), ToolCall{Name: "echo"})
	if err == nil {
		t.Fatalf("expected caller error")
	}

	_, err = reg.Execute(context.Background(), ToolCall{Name: "echo", Caller: &ToolCaller{Type: "programmatic"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistryExecute(t *testing.T) {
	reg := NewRegistry()
	payload := json.RawMessage(`{"ok":true}`)
	reg.Register(echoTool{def: ToolDefinition{Name: "echo"}})

	result, err := reg.Execute(context.Background(), ToolCall{Name: "echo", Input: payload})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Output) != string(payload) {
		t.Fatalf("expected output to echo input")
	}
}
