package history

import (
	"context"
	"testing"

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
}
