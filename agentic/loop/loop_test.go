package loop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/history"
	"github.com/victorarias/agentic-weave/agentic/truncate"
)

type stepDecider struct {
	calls int
}

func (d *stepDecider) Decide(ctx context.Context, in Input) (Decision, error) {
	if d.calls == 0 {
		d.calls++
		return Decision{
			ToolCalls: []agentic.ToolCall{{
				Name:  "echo",
				Input: json.RawMessage(`{"text":"hello"}`),
			}},
		}, nil
	}
	return Decision{Reply: "done"}, nil
}

type stubExecutor struct{}

func (stubExecutor) ListTools(ctx context.Context) ([]agentic.ToolDefinition, error) {
	return []agentic.ToolDefinition{{Name: "echo", Description: "echo tool"}}, nil
}

func (stubExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{Name: call.Name, Output: []byte("one\ntwo")}, nil
}

func TestRunWithToolAndTruncation(t *testing.T) {
	var eventsSeen []string
	sink := events.SinkFunc(func(e events.Event) {
		eventsSeen = append(eventsSeen, e.Type)
	})

	runner := New(Config{
		Decider:        &stepDecider{},
		Executor:       stubExecutor{},
		Truncation:     &truncate.Options{MaxLines: 1, MaxBytes: 100},
		TruncationMode: truncate.ModeTail,
		Events:         sink,
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "done" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result")
	}
	if string(result.ToolResults[0].Output) != "two" {
		t.Fatalf("expected truncated output, got %q", string(result.ToolResults[0].Output))
	}
	foundTruncate := false
	for _, event := range eventsSeen {
		if event == events.ToolOutputTruncated {
			foundTruncate = true
		}
	}
	if !foundTruncate {
		t.Fatalf("expected truncation event")
	}
}

func TestRunWithCompaction(t *testing.T) {
	store := history.NewMemoryStore()
	_ = store.Append(context.Background(), budget.Message{Role: "user", Content: "hello there"})
	_ = store.Append(context.Background(), budget.Message{Role: "assistant", Content: "general kenobi"})

	compactor := &recordingCompactor{summary: "summary"}
	budgetMgr := &budget.Manager{
		Counter:   budget.CharCounter{},
		Compactor: compactor,
		Policy: budget.Policy{
			ContextWindow: 4,
			ReserveTokens: 0,
			KeepLast:      1,
		},
	}

	var eventsSeen []string
	sink := events.SinkFunc(func(e events.Event) {
		eventsSeen = append(eventsSeen, e.Type)
	})

	runner := New(Config{
		Decider:      &replyDecider{reply: "ok"},
		HistoryStore: store,
		Budget:       budgetMgr,
		Events:       sink,
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "ping"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary != "summary" {
		t.Fatalf("expected summary")
	}

	msgs, _ := store.Load(context.Background())
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatalf("expected compacted history in store")
	}

	foundStart := false
	foundEnd := false
	for _, event := range eventsSeen {
		if event == events.ContextCompactionStart {
			foundStart = true
		}
		if event == events.ContextCompactionEnd {
			foundEnd = true
		}
	}
	if !foundStart || !foundEnd {
		t.Fatalf("expected compaction events")
	}
}

type replyDecider struct {
	reply string
}

func (r *replyDecider) Decide(ctx context.Context, in Input) (Decision, error) {
	return Decision{Reply: r.reply}, nil
}

type recordingCompactor struct {
	summary string
	last    []budget.Message
}

func (r *recordingCompactor) Compact(ctx context.Context, messages []budget.Message) (string, error) {
	r.last = append([]budget.Message(nil), messages...)
	return r.summary, nil
}
