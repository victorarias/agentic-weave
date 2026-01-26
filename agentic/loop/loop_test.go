package loop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/history"
	"github.com/victorarias/agentic-weave/agentic/message"
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
	_ = store.Append(context.Background(), message.AgentMessage{Role: "user", Content: "hello there"})
	_ = store.Append(context.Background(), message.AgentMessage{Role: "assistant", Content: "general kenobi"})

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

func TestRunWithCompactionRequiresRewriter(t *testing.T) {
	store := &appendOnlyStore{}
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

	runner := New(Config{
		Decider:      &replyDecider{reply: "ok"},
		HistoryStore: store,
		Budget:       budgetMgr,
	})

	_, err := runner.Run(context.Background(), Request{UserMessage: "ping"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "history.Rewriter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPersistsToolHistory(t *testing.T) {
	store := history.NewMemoryStore()
	// Pre-populate with an assistant message containing a prior tool call
	_ = store.Append(context.Background(), message.AgentMessage{
		Role: message.RoleAssistant,
		ToolCalls: []agentic.ToolCall{
			{ID: "prior-1", Name: "prior"},
		},
	})
	// And a tool result message
	_ = store.Append(context.Background(), message.AgentMessage{
		Role: message.RoleTool,
		ToolResults: []agentic.ToolResult{
			{ID: "prior-1", Name: "prior", Output: []byte("ok")},
		},
	})

	decider := &historyAssertingDecider{t: t}
	runner := New(Config{
		Decider:      decider,
		Executor:     stubExecutor{},
		HistoryStore: store,
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should have 2 tool calls (prior + new)
	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "prior" {
		t.Fatalf("expected prior tool call first, got %q", result.ToolCalls[0].Name)
	}
	if result.ToolCalls[1].ID != "call-0-0" {
		t.Fatalf("expected generated call id, got %q", result.ToolCalls[1].ID)
	}

	// Check messages in store include tool calls and results as structured data
	msgs, _ := store.Load(context.Background())
	var foundToolCallInAssistant, foundToolResultInTool bool
	for _, m := range msgs {
		if m.Role == message.RoleAssistant && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				if tc.Name == "echo" {
					foundToolCallInAssistant = true
				}
			}
		}
		if m.Role == message.RoleTool && len(m.ToolResults) > 0 {
			for _, tr := range m.ToolResults {
				if tr.Name == "echo" {
					foundToolResultInTool = true
				}
			}
		}
	}
	if !foundToolCallInAssistant {
		t.Fatal("expected tool call to be stored in assistant message")
	}
	if !foundToolResultInTool {
		t.Fatal("expected tool result to be stored in tool message")
	}
}

func TestMessageEndEventWithToolCalls(t *testing.T) {
	var evts []events.Event
	sink := events.SinkFunc(func(e events.Event) {
		evts = append(evts, e)
	})

	runner := New(Config{
		Decider:  &stepDecider{},
		Executor: stubExecutor{},
		Events:   sink,
	})

	_, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var messageEndEvents []events.Event
	for _, e := range evts {
		if e.Type == events.MessageEnd {
			messageEndEvents = append(messageEndEvents, e)
		}
	}

	if len(messageEndEvents) != 2 {
		t.Fatalf("expected 2 MessageEnd events, got %d", len(messageEndEvents))
	}

	// First MessageEnd should have tool calls
	if len(messageEndEvents[0].ToolCalls) != 1 {
		t.Fatalf("expected tool calls in first MessageEnd event, got %d", len(messageEndEvents[0].ToolCalls))
	}
	if messageEndEvents[0].ToolCalls[0].Name != "echo" {
		t.Fatalf("expected echo tool call, got %q", messageEndEvents[0].ToolCalls[0].Name)
	}

	// Second MessageEnd is the final reply (no tool calls)
	if len(messageEndEvents[1].ToolCalls) != 0 {
		t.Fatalf("expected no tool calls in final MessageEnd event")
	}
	if messageEndEvents[1].Content != "done" {
		t.Fatalf("expected final content 'done', got %q", messageEndEvents[1].Content)
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
	last    []budget.Budgetable
}

func (r *recordingCompactor) Compact(ctx context.Context, messages []budget.Budgetable) (string, error) {
	r.last = append([]budget.Budgetable(nil), messages...)
	return r.summary, nil
}

type appendOnlyStore struct {
	messages []message.AgentMessage
}

func (s *appendOnlyStore) Append(ctx context.Context, msg message.AgentMessage) error {
	s.messages = append(s.messages, msg)
	return nil
}

func (s *appendOnlyStore) Load(ctx context.Context) ([]message.AgentMessage, error) {
	out := make([]message.AgentMessage, len(s.messages))
	copy(out, s.messages)
	return out, nil
}

type historyAssertingDecider struct {
	t     *testing.T
	calls int
}

func (d *historyAssertingDecider) Decide(ctx context.Context, in Input) (Decision, error) {
	if d.calls == 0 {
		d.calls++
		if len(in.ToolCalls) != 1 || in.ToolCalls[0].Name != "prior" {
			d.t.Fatalf("expected prior tool call, got %#v", in.ToolCalls)
		}
		if len(in.ToolResults) != 1 || in.ToolResults[0].Name != "prior" {
			d.t.Fatalf("expected prior tool result, got %#v", in.ToolResults)
		}
		return Decision{
			ToolCalls: []agentic.ToolCall{{Name: "echo"}},
		}, nil
	}
	return Decision{Reply: "done"}, nil
}
