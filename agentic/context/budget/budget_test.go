package budget

import (
	"context"
	"testing"
)

type charCounter struct{}

func (charCounter) Count(text string) int {
	return len(text)
}

type recordingCompactor struct {
	summary string
	last    []Message
}

func (r *recordingCompactor) Compact(ctx context.Context, messages []Message) (string, error) {
	r.last = append([]Message(nil), messages...)
	return r.summary, nil
}

func TestNoOpWithoutDependencies(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hello"}}
	mgr := Manager{Policy: Policy{ContextWindow: 10}}

	out, summary, changed, err := mgr.CompactIfNeeded(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed || summary != "" {
		t.Fatalf("expected no changes")
	}
	if len(out) != len(msgs) {
		t.Fatalf("expected messages unchanged")
	}
}

func TestCharCounterDefault(t *testing.T) {
	counter := CharCounter{}
	if counter.Count("1234") != 1 {
		t.Fatalf("expected 1 token for 4 chars")
	}
	if counter.Count("12345") != 2 {
		t.Fatalf("expected 2 tokens for 5 chars")
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []Message{{Content: "abcd"}, {Content: "efgh"}}
	total := EstimateTokens(msgs, CharCounter{})
	if total != 2 {
		t.Fatalf("expected 2 tokens, got %d", total)
	}
}

func TestNoCompactionBelowThreshold(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hello"}}
	compactor := &recordingCompactor{summary: "summary"}
	mgr := Manager{
		Counter:   charCounter{},
		Compactor: compactor,
		Policy:    Policy{ContextWindow: 10, ReserveTokens: 2},
	}

	out, summary, changed, err := mgr.CompactIfNeeded(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed || summary != "" {
		t.Fatalf("expected no changes")
	}
	if len(out) != len(msgs) {
		t.Fatalf("expected messages unchanged")
	}
}

func TestCompactionKeepsRecentTokens(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "aaa"},
		{Role: "assistant", Content: "bbbb"},
		{Role: "user", Content: "cc"},
		{Role: "assistant", Content: "ddddd"},
	}
	compactor := &recordingCompactor{summary: "summary"}
	mgr := Manager{
		Counter:   charCounter{},
		Compactor: compactor,
		Policy: Policy{
			ContextWindow:    10,
			ReserveTokens:    0,
			KeepRecentTokens: 6,
		},
	}

	out, summary, changed, err := mgr.CompactIfNeeded(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("expected compaction")
	}
	if summary != "summary" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	if out[0].Role != "system" || out[0].Content != "summary" {
		t.Fatalf("expected summary system message")
	}
	if len(compactor.last) != 2 {
		t.Fatalf("expected 2 compacted messages, got %d", len(compactor.last))
	}
}

func TestCompactionKeepLastFallback(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "aaa"},
		{Role: "assistant", Content: "bbbb"},
		{Role: "user", Content: "cc"},
	}
	compactor := &recordingCompactor{summary: "summary"}
	mgr := Manager{
		Counter:   charCounter{},
		Compactor: compactor,
		Policy: Policy{
			ContextWindow: 5,
			ReserveTokens: 0,
			KeepLast:      1,
		},
	}

	out, summary, changed, err := mgr.CompactIfNeeded(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed || summary == "" {
		t.Fatalf("expected compaction")
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if len(compactor.last) != 2 {
		t.Fatalf("expected 2 compacted messages, got %d", len(compactor.last))
	}
}

func TestNoCompactionWhenKeepRecentTooLarge(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "aaa"},
		{Role: "assistant", Content: "bbbb"},
	}
	compactor := &recordingCompactor{summary: "summary"}
	mgr := Manager{
		Counter:   charCounter{},
		Compactor: compactor,
		Policy: Policy{
			ContextWindow:    5,
			ReserveTokens:    0,
			KeepRecentTokens: 100,
		},
	}

	out, summary, changed, err := mgr.CompactIfNeeded(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed || summary != "" {
		t.Fatalf("expected no changes")
	}
	if len(out) != len(msgs) {
		t.Fatalf("expected messages unchanged")
	}
}
