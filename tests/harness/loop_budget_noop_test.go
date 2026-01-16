package harness

import (
	"context"
	"testing"

	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/history"
	"github.com/victorarias/agentic-weave/agentic/loop"
)

func TestLoopBudgetNoOpPaths(t *testing.T) {
	cases := []struct {
		name string
		mgr  *budget.Manager
	}{
		{
			name: "missing counter",
			mgr: &budget.Manager{
				Compactor: recordingCompactor{summary: "summary"},
				Policy: budget.Policy{
					ContextWindow: 1,
					KeepLast:      1,
				},
			},
		},
		{
			name: "missing compactor",
			mgr: &budget.Manager{
				Counter: budget.CharCounter{},
				Policy: budget.Policy{
					ContextWindow: 1,
					KeepLast:      1,
				},
			},
		},
		{
			name: "missing context window",
			mgr: &budget.Manager{
				Counter:   budget.CharCounter{},
				Compactor: recordingCompactor{summary: "summary"},
				Policy: budget.Policy{
					ContextWindow: 0,
					KeepLast:      1,
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			store := history.NewMemoryStore()
			_ = store.Append(context.Background(), budget.Message{Role: "user", Content: "hello"})
			_ = store.Append(context.Background(), budget.Message{Role: "assistant", Content: "world"})

			decider := &scriptedDecider{
				script: []loop.Decision{{Reply: "ok"}},
			}

			result, eventsSeen, err := runScenario(t, loop.Config{
				Decider:      decider,
				HistoryStore: store,
				Budget:       tt.mgr,
			}, loop.Request{UserMessage: "ping"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Summary != "" {
				t.Fatalf("expected empty summary, got %q", result.Summary)
			}
			if len(decider.inputs) == 0 || len(decider.inputs[0].History) == 0 {
				t.Fatalf("expected history")
			}
			if decider.inputs[0].History[0].Content == "summary" {
				t.Fatalf("unexpected summary injection")
			}
			foundEnd := false
			for _, event := range eventsSeen {
				if event.Type == events.ContextCompactionEnd {
					foundEnd = true
				}
			}
			if foundEnd {
				t.Fatalf("unexpected compaction end event")
			}
		})
	}
}
