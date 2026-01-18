package context

import (
	"context"
	"testing"
)

type fixedCounter struct{ count int }

func (f fixedCounter) Count(text string) int { return f.count }

func TestCompactWithSystemPreservesSystem(t *testing.T) {
	mgr := Manager{
		Counter:     fixedCounter{count: 10},
		MaxTokens:   5,
		KeepLast:    1,
		CompactFunc: func(ctx context.Context, messages []Message) (string, error) {
			return "summary", nil
		},
	}
	messages := []Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "world"}}
	system := Message{Role: "system", Content: "system prompt"}

	compacted, summary, err := CompactWithSystem(context.Background(), system, messages, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary == "" {
		t.Fatalf("expected summary")
	}
	if len(compacted) == 0 || compacted[0].Role != "system" {
		t.Fatalf("expected system prompt first")
	}
}
