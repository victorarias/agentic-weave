package harness

import (
	"context"
	"testing"

	"github.com/victorarias/agentic-weave/agentic/history"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/message"
)

func TestLoopBasicReplyHistory(t *testing.T) {
	decider := &scriptedDecider{
		script: []loop.Decision{{Reply: "ok"}},
	}
	reqHistory := []message.AgentMessage{{Role: message.RoleSystem, Content: "seed"}}
	result, _, err := runScenario(t, loop.Config{
		Decider: decider,
	}, loop.Request{
		UserMessage: "hi",
		History:     reqHistory,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "ok" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
	if len(result.History) != 3 {
		t.Fatalf("expected 3 history messages, got %d", len(result.History))
	}
	if len(decider.inputs) != 1 {
		t.Fatalf("expected 1 decider call, got %d", len(decider.inputs))
	}
	if len(decider.inputs[0].History) != 2 {
		t.Fatalf("expected decider history to include seed + user")
	}
	if decider.inputs[0].History[0].Content != "seed" {
		t.Fatalf("unexpected history seed: %q", decider.inputs[0].History[0].Content)
	}
}

func TestLoopHistoryStoreOverridesRequest(t *testing.T) {
	store := history.NewMemoryStore()
	_ = store.Append(context.Background(), message.AgentMessage{Role: message.RoleSystem, Content: "from-store"})

	decider := &scriptedDecider{
		script: []loop.Decision{{Reply: "ok"}},
	}
	result, _, err := runScenario(t, loop.Config{
		Decider:      decider,
		HistoryStore: store,
	}, loop.Request{
		UserMessage: "hi",
		History:     []message.AgentMessage{{Role: message.RoleSystem, Content: "from-request"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decider.inputs) == 0 {
		t.Fatalf("expected decider input")
	}
	if decider.inputs[0].History[0].Content != "from-store" {
		t.Fatalf("expected history from store, got %q", decider.inputs[0].History[0].Content)
	}
	if len(result.History) == 0 || result.History[0].Content != "from-store" {
		t.Fatalf("expected result history from store")
	}
}
