package harness

import (
	"context"
	"testing"

	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/history"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/message"
)

type recordingCompactor struct {
	summary string
}

func (r recordingCompactor) Compact(ctx context.Context, messages []budget.Message) (string, error) {
	return r.summary, nil
}

func TestLoopCompactionInjectedHistory(t *testing.T) {
	store := history.NewMemoryStore()
	_ = store.Append(context.Background(), message.AgentMessage{Role: message.RoleUser, Content: "hello"})
	_ = store.Append(context.Background(), message.AgentMessage{Role: message.RoleAssistant, Content: "world"})

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

	result, eventsSeen, err := runScenario(t, loop.Config{
		Decider:      decider,
		HistoryStore: store,
		Budget:       budgetMgr,
	}, loop.Request{UserMessage: "ping"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary != "summary" {
		t.Fatalf("expected summary, got %q", result.Summary)
	}
	if len(decider.inputs) == 0 || len(decider.inputs[0].History) == 0 {
		t.Fatalf("expected decider history")
	}
	if decider.inputs[0].History[0].Role != message.RoleSystem || decider.inputs[0].History[0].Content != "summary" {
		t.Fatalf("expected compaction summary in history")
	}
	foundStart := false
	foundEnd := false
	for _, event := range eventsSeen {
		if event.Type == events.ContextCompactionStart {
			foundStart = true
		}
		if event.Type == events.ContextCompactionEnd {
			foundEnd = true
		}
	}
	if !foundStart || !foundEnd {
		t.Fatalf("expected compaction events")
	}
}
