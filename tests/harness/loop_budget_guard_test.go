package harness

import (
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/loop"
)

func TestLoopCompactionRequiresRewriter(t *testing.T) {
	store := &appendOnlyStore{}
	budgetMgr := &budget.Manager{
		Counter:   budget.CharCounter{},
		Compactor: recordingCompactor{summary: "summary"},
		Policy: budget.Policy{
			ContextWindow: 1,
			KeepLast:      1,
		},
	}
	decider := &scriptedDecider{
		script: []loop.Decision{{Reply: "ok"}},
	}

	_, _, err := runScenario(t, loop.Config{
		Decider:      decider,
		HistoryStore: store,
		Budget:       budgetMgr,
	}, loop.Request{UserMessage: "ping"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "history.Rewriter") {
		t.Fatalf("unexpected error: %v", err)
	}
}
