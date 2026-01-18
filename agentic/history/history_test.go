package history

import (
	"context"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
)

func TestMemoryStore(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Append(context.Background(), budget.Message{Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("append failed: %v", err)
	}
	msgs, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Fatalf("unexpected messages: %#v", msgs)
	}

	if err := store.Replace(context.Background(), []budget.Message{{Role: "system", Content: "summary"}}); err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	msgs, _ = store.Load(context.Background())
	if len(msgs) != 1 || msgs[0].Role != "system" {
		t.Fatalf("unexpected messages after replace: %#v", msgs)
	}

	if err := store.AppendToolCall(context.Background(), agentic.ToolCall{Name: "echo"}); err != nil {
		t.Fatalf("append tool call failed: %v", err)
	}
	if err := store.AppendToolResult(context.Background(), agentic.ToolResult{Name: "echo"}); err != nil {
		t.Fatalf("append tool result failed: %v", err)
	}
	calls, err := store.LoadToolCalls(context.Background())
	if err != nil {
		t.Fatalf("load tool calls failed: %v", err)
	}
	results, err := store.LoadToolResults(context.Background())
	if err != nil {
		t.Fatalf("load tool results failed: %v", err)
	}
	if len(calls) != 1 || calls[0].Name != "echo" {
		t.Fatalf("unexpected tool calls: %#v", calls)
	}
	if len(results) != 1 || results[0].Name != "echo" {
		t.Fatalf("unexpected tool results: %#v", results)
	}
}
