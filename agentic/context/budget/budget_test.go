package budget

import (
	"context"
	"testing"
)

// testMessage implements Budgetable for tests.
type testMessage struct {
	role    string
	content string
}

func (m testMessage) BudgetRole() string    { return m.role }
func (m testMessage) BudgetContent() string { return m.content }

type charCounter struct{}

func (charCounter) Count(text string) int {
	return len(text)
}

type recordingCompactor struct {
	summary string
	last    []Budgetable
}

func (r *recordingCompactor) Compact(ctx context.Context, messages []Budgetable) (string, error) {
	r.last = append([]Budgetable(nil), messages...)
	return r.summary, nil
}

func TestNoOpWithoutDependencies(t *testing.T) {
	msgs := []Budgetable{testMessage{role: "user", content: "hello"}}
	mgr := Manager{Policy: Policy{ContextWindow: 10}}

	summary, keepCount, changed, err := mgr.CompactIfNeeded(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed || summary != "" {
		t.Fatalf("expected no changes")
	}
	if keepCount != len(msgs) {
		t.Fatalf("expected keepCount %d, got %d", len(msgs), keepCount)
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
	msgs := []testMessage{{content: "abcd"}, {content: "efgh"}}
	total := EstimateTokens(msgs, CharCounter{})
	if total != 2 {
		t.Fatalf("expected 2 tokens, got %d", total)
	}
}

func TestNoCompactionBelowThreshold(t *testing.T) {
	msgs := []Budgetable{testMessage{role: "user", content: "hello"}}
	compactor := &recordingCompactor{summary: "summary"}
	mgr := Manager{
		Counter:   charCounter{},
		Compactor: compactor,
		Policy:    Policy{ContextWindow: 10, ReserveTokens: 2},
	}

	summary, keepCount, changed, err := mgr.CompactIfNeeded(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed || summary != "" {
		t.Fatalf("expected no changes")
	}
	if keepCount != len(msgs) {
		t.Fatalf("expected keepCount %d, got %d", len(msgs), keepCount)
	}
}

func TestCompactionKeepsRecentTokens(t *testing.T) {
	msgs := []Budgetable{
		testMessage{role: "user", content: "aaa"},
		testMessage{role: "assistant", content: "bbbb"},
		testMessage{role: "user", content: "cc"},
		testMessage{role: "assistant", content: "ddddd"},
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

	summary, keepCount, changed, err := mgr.CompactIfNeeded(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("expected compaction")
	}
	if summary != "summary" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if keepCount != 2 {
		t.Fatalf("expected keepCount 2, got %d", keepCount)
	}
	if len(compactor.last) != 2 {
		t.Fatalf("expected 2 compacted messages, got %d", len(compactor.last))
	}
}

func TestCompactionKeepLastFallback(t *testing.T) {
	msgs := []Budgetable{
		testMessage{role: "user", content: "aaa"},
		testMessage{role: "assistant", content: "bbbb"},
		testMessage{role: "user", content: "cc"},
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

	summary, keepCount, changed, err := mgr.CompactIfNeeded(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed || summary == "" {
		t.Fatalf("expected compaction")
	}
	if keepCount != 1 {
		t.Fatalf("expected keepCount 1, got %d", keepCount)
	}
	if len(compactor.last) != 2 {
		t.Fatalf("expected 2 compacted messages, got %d", len(compactor.last))
	}
}

func TestNoCompactionWhenKeepRecentTooLarge(t *testing.T) {
	msgs := []Budgetable{
		testMessage{role: "user", content: "aaa"},
		testMessage{role: "assistant", content: "bbbb"},
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

	summary, keepCount, changed, err := mgr.CompactIfNeeded(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed || summary != "" {
		t.Fatalf("expected no changes")
	}
	if keepCount != len(msgs) {
		t.Fatalf("expected keepCount %d, got %d", len(msgs), keepCount)
	}
}
