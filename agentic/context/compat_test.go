package context

import (
	stdctx "context"
	"testing"

	"github.com/victorarias/agentic-weave/agentic/context/budget"
)

func TestToBudgetRoundTrip(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}}
	budgetMsgs := ToBudgetMessages(msgs)
	back := FromBudgetMessages(budgetMsgs)
	if len(back) != 1 || back[0].Content != "hi" {
		t.Fatalf("unexpected round trip: %#v", back)
	}
}

func TestToBudgetManager(t *testing.T) {
	mgr := Manager{
		MaxTokens: 5,
		KeepLast:  1,
		Counter:   charCounter{},
		CompactFunc: func(ctx stdctx.Context, messages []Message) (string, error) {
			return "summary", nil
		},
	}

	budgetMgr := mgr.ToBudget(budget.Policy{})
	msgs := []budget.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "world"}}
	out, summary, changed, err := budgetMgr.CompactIfNeeded(stdctx.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed || summary == "" || len(out) == 0 {
		t.Fatalf("expected compaction")
	}
}

func TestToBudgetManagerNoCompactor(t *testing.T) {
	mgr := Manager{
		MaxTokens: 5,
		KeepLast:  1,
		Counter:   charCounter{},
	}

	budgetMgr := mgr.ToBudget(budget.Policy{})
	msgs := []budget.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "world"}}
	out, summary, changed, err := budgetMgr.CompactIfNeeded(stdctx.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed || summary != "" || len(out) != len(msgs) {
		t.Fatalf("expected no compaction")
	}
}

type charCounter struct{}

func (charCounter) Count(text string) int { return len(text) }
