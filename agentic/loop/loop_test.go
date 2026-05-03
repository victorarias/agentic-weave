package loop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/history"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/truncate"
	"github.com/victorarias/agentic-weave/agentic/usage"
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
	if !strings.HasPrefix(result.ToolCalls[1].ID, "call-") {
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

func TestExhausted_FalseOnNaturalStop(t *testing.T) {
	runner := New(Config{
		Decider:  &replyDecider{reply: "done"},
		MaxTurns: 10,
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Exhausted {
		t.Fatal("expected Exhausted=false when LLM stops naturally")
	}
}

func TestExhausted_TrueWhenMaxTurnsReached(t *testing.T) {
	runner := New(Config{
		Decider:  &alwaysCallToolDecider{},
		Executor: stubExecutor{},
		MaxTurns: 3,
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Exhausted {
		t.Fatal("expected Exhausted=true when loop hits MaxTurns")
	}
}

func TestExhausted_PreservesPendingToolCallsAtBoundary(t *testing.T) {
	runner := New(Config{
		Decider:  &alwaysCallToolDecider{},
		Executor: stubExecutor{},
		MaxTurns: 1,
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Exhausted {
		t.Fatal("expected Exhausted=true when loop hits MaxTurns")
	}
	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected executed+pending tool calls, got %d", len(result.ToolCalls))
	}
	last := result.History[len(result.History)-1]
	if last.Role != message.RoleAssistant || len(last.ToolCalls) == 0 {
		t.Fatalf("expected final assistant message with pending tool calls, got %#v", last)
	}
}

func TestRunMergesRequestHistoryWithStoreHistory(t *testing.T) {
	store := history.NewMemoryStore()
	_ = store.Append(context.Background(), message.AgentMessage{Role: message.RoleSystem, Content: "from-store"})
	decider := &historyMergeDecider{t: t}
	runner := New(Config{
		Decider:      decider,
		HistoryStore: store,
	})

	_, err := runner.Run(context.Background(), Request{
		UserMessage: "hi",
		History: []message.AgentMessage{
			{Role: message.RoleSystem, Content: "from-request"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReturnsErrorWhenHistoryAppendFails(t *testing.T) {
	store := &failingAppendStore{err: errors.New("append failed")}
	runner := New(Config{
		Decider:      &replyDecider{reply: "done"},
		HistoryStore: store,
	})

	_, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err == nil || !strings.Contains(err.Error(), "append failed") {
		t.Fatalf("expected append error, got %v", err)
	}
}

func TestRunReturnsErrorWhenHistoryReplaceFails(t *testing.T) {
	store := &failingReplaceStore{
		err: errors.New("replace failed"),
		messages: []message.AgentMessage{
			{Role: message.RoleUser, Content: "hello there"},
			{Role: message.RoleAssistant, Content: "general kenobi"},
		},
	}
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
	if err == nil || !strings.Contains(err.Error(), "replace failed") {
		t.Fatalf("expected replace error, got %v", err)
	}
}

func TestRunBeforeNextModelCallAppendsHookMessagesBeforeDecide(t *testing.T) {
	store := history.NewMemoryStore()
	decider := &capturingDecider{replies: []string{"done"}}
	hookReturned := false
	runner := New(Config{
		Decider:      decider,
		HistoryStore: store,
		BeforeNextModelCall: BeforeNextModelCallHookFunc(func(_ context.Context, in BeforeNextModelCallInput) ([]message.AgentMessage, error) {
			if hookReturned {
				return nil, nil
			}
			hookReturned = true
			if in.Turn != 0 {
				t.Fatalf("expected turn 0, got %d", in.Turn)
			}
			if len(in.History) != 1 || in.History[0].Content != "hi" {
				t.Fatalf("expected initial user message in hook history, got %#v", in.History)
			}
			in.History[0].Content = "mutated by hook"
			return []message.AgentMessage{{Role: message.RoleUser, Content: "steer now"}}, nil
		}),
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "done" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
	if len(decider.inputs) != 1 {
		t.Fatalf("expected one decide call, got %d", len(decider.inputs))
	}
	gotHistory := decider.inputs[0].History
	if len(gotHistory) != 2 || gotHistory[0].Content != "hi" || gotHistory[1].Content != "steer now" {
		t.Fatalf("expected hook message appended before decide, got %#v", gotHistory)
	}

	stored, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	if len(stored) != 3 || stored[1].Content != "steer now" || stored[2].Content != "done" {
		t.Fatalf("expected hook message persisted before final assistant, got %#v", stored)
	}
}

func TestRunBeforeNextModelCallDeepCopiesHookInput(t *testing.T) {
	decider := &nestedHistoryAssertingDecider{t: t}
	hookReturned := false
	runner := New(Config{
		Decider:  decider,
		Executor: schemaExecutor{},
		BeforeNextModelCall: BeforeNextModelCallHookFunc(func(_ context.Context, in BeforeNextModelCallInput) ([]message.AgentMessage, error) {
			if hookReturned {
				return nil, nil
			}
			hookReturned = true

			in.History[0].ToolCalls[0].Input[0] = 'X'
			in.History[0].ToolCalls[0].Caller.Type = "mutated"
			in.History[1].ToolResults[0].Output[0] = 'X'
			in.History[1].ToolResults[0].InlineData[0].Data[0] = 9
			in.History[1].ToolResults[0].Error.Message = "mutated"
			in.Tools[0].InputSchema[0] = 'X'
			in.Tools[0].Examples[0].Input[0] = 'X'
			in.Tools[0].Examples[0].Output[0] = 'X'
			in.ToolCalls[0].Input[0] = 'X'
			in.ToolResults[0].Output[0] = 'X'
			return nil, nil
		}),
	})

	_, err := runner.Run(context.Background(), Request{
		UserMessage: "hi",
		History: []message.AgentMessage{
			{
				Role: message.RoleAssistant,
				ToolCalls: []agentic.ToolCall{{
					ID:     "prior-1",
					Name:   "prior",
					Input:  json.RawMessage(`{"a":1}`),
					Caller: &agentic.ToolCaller{Type: "original"},
				}},
			},
			{
				Role: message.RoleTool,
				ToolResults: []agentic.ToolResult{{
					ID:         "prior-1",
					Name:       "prior",
					Output:     json.RawMessage(`{"ok":true}`),
					InlineData: []agentic.InlineData{{MIMEType: "image/png", Data: []byte{1, 2, 3}}},
					Error:      &agentic.ToolError{Message: "original"},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunBeforeNextModelCallContinuesAfterFinalReplyWhenHookReturnsMessages(t *testing.T) {
	decider := &capturingDecider{replies: []string{"first reply", "second reply"}}
	hookCalls := 0
	runner := New(Config{
		Decider: decider,
		BeforeNextModelCall: BeforeNextModelCallHookFunc(func(_ context.Context, in BeforeNextModelCallInput) ([]message.AgentMessage, error) {
			hookCalls++
			if hookCalls == 2 {
				last := in.History[len(in.History)-1]
				if last.Role != message.RoleAssistant || last.Content != "first reply" {
					t.Fatalf("expected post-final hook history to include pending assistant reply, got %#v", last)
				}
				return []message.AgentMessage{{Role: message.RoleUser, Content: "steer after first reply"}}, nil
			}
			return nil, nil
		}),
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "second reply" {
		t.Fatalf("expected second reply, got %q", result.Reply)
	}
	if result.Steps != 2 {
		t.Fatalf("expected two decide calls, got %d", result.Steps)
	}
	if len(decider.inputs) != 2 {
		t.Fatalf("expected two captured inputs, got %d", len(decider.inputs))
	}

	secondHistory := decider.inputs[1].History
	if len(secondHistory) != 3 {
		t.Fatalf("expected user, first assistant, steer before second decide; got %#v", secondHistory)
	}
	if secondHistory[1].Role != message.RoleAssistant || secondHistory[1].Content != "first reply" {
		t.Fatalf("expected first assistant reply to remain in history, got %#v", secondHistory[1])
	}
	if secondHistory[2].Role != message.RoleUser || secondHistory[2].Content != "steer after first reply" {
		t.Fatalf("expected steering message before second decide, got %#v", secondHistory[2])
	}
}

func TestRunBeforeNextModelCallDoesNotSpendToolBudget(t *testing.T) {
	decider := &replyThenToolDecider{}
	hookCalls := 0
	runner := New(Config{
		Decider:  decider,
		Executor: stubExecutor{},
		MaxTurns: 1,
		BeforeNextModelCall: BeforeNextModelCallHookFunc(func(_ context.Context, in BeforeNextModelCallInput) ([]message.AgentMessage, error) {
			hookCalls++
			if hookCalls == 2 {
				if !in.CanContinue {
					t.Fatal("expected continuation budget after first reply")
				}
				return []message.AgentMessage{{Role: message.RoleUser, Content: "steer into tools"}}, nil
			}
			return nil, nil
		}),
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "done" {
		t.Fatalf("expected final reply, got %q", result.Reply)
	}
	if result.Exhausted {
		t.Fatal("expected hook-only continuation not to exhaust tool budget")
	}
	if result.Steps != 3 {
		t.Fatalf("expected first reply, tool decision, final reply; got %d steps", result.Steps)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected tool call after steering to execute, got %d results", len(result.ToolResults))
	}
}

func TestRunBeforeNextModelCallStopsRogueHookAtContinuationLimit(t *testing.T) {
	decider := &capturingDecider{replies: []string{"first reply", "second reply", "third reply"}}
	hookCalls := 0
	sawContinuationBlocked := false
	runner := New(Config{
		Decider:  decider,
		MaxTurns: 1,
		BeforeNextModelCall: BeforeNextModelCallHookFunc(func(_ context.Context, in BeforeNextModelCallInput) ([]message.AgentMessage, error) {
			hookCalls++
			if !in.CanContinue {
				sawContinuationBlocked = true
			}
			if hookCalls%2 == 0 {
				return []message.AgentMessage{{Role: message.RoleUser, Content: "keep going"}}, nil
			}
			return nil, nil
		}),
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "second reply" {
		t.Fatalf("expected second reply, got %q", result.Reply)
	}
	if !result.Exhausted {
		t.Fatal("expected exhausted result when hook continuation reaches MaxTurns")
	}
	if result.Steps != 2 {
		t.Fatalf("expected two decide calls, got %d", result.Steps)
	}
	if len(decider.inputs) != 2 {
		t.Fatalf("expected hook to stop before a third decide call, got %d", len(decider.inputs))
	}
	if hookCalls != 4 {
		t.Fatalf("expected final hook call to expose blocked continuation budget, got %d calls", hookCalls)
	}
	if !sawContinuationBlocked {
		t.Fatal("expected hook input to report no continuation budget")
	}
}

func TestRunBeforeNextModelCallReturnsError(t *testing.T) {
	hookErr := errors.New("hook failed")
	runner := New(Config{
		Decider: &replyDecider{reply: "done"},
		BeforeNextModelCall: BeforeNextModelCallHookFunc(func(context.Context, BeforeNextModelCallInput) ([]message.AgentMessage, error) {
			return nil, hookErr
		}),
	})

	_, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected hook error, got %v", err)
	}
}

type alwaysCallToolDecider struct{}

func (d *alwaysCallToolDecider) Decide(ctx context.Context, in Input) (Decision, error) {
	return Decision{
		ToolCalls: []agentic.ToolCall{{
			Name:  "echo",
			Input: json.RawMessage(`{"text":"again"}`),
		}},
	}, nil
}

type replyDecider struct {
	reply string
}

func (r *replyDecider) Decide(ctx context.Context, in Input) (Decision, error) {
	return Decision{Reply: r.reply}, nil
}

type capturingDecider struct {
	replies []string
	inputs  []Input
}

func (d *capturingDecider) Decide(_ context.Context, in Input) (Decision, error) {
	in.History = append([]message.AgentMessage(nil), in.History...)
	in.Tools = append([]agentic.ToolDefinition(nil), in.Tools...)
	in.ToolCalls = append([]agentic.ToolCall(nil), in.ToolCalls...)
	in.ToolResults = append([]agentic.ToolResult(nil), in.ToolResults...)
	d.inputs = append(d.inputs, in)
	if len(d.replies) == 0 {
		return Decision{Reply: "done"}, nil
	}
	reply := d.replies[0]
	d.replies = d.replies[1:]
	return Decision{Reply: reply}, nil
}

type replyThenToolDecider struct {
	calls int
}

func (d *replyThenToolDecider) Decide(_ context.Context, _ Input) (Decision, error) {
	d.calls++
	switch d.calls {
	case 1:
		return Decision{Reply: "first reply"}, nil
	case 2:
		return Decision{ToolCalls: []agentic.ToolCall{{Name: "echo"}}}, nil
	default:
		return Decision{Reply: "done"}, nil
	}
}

type nestedHistoryAssertingDecider struct {
	t *testing.T
}

func (d *nestedHistoryAssertingDecider) Decide(_ context.Context, in Input) (Decision, error) {
	if got := string(in.History[0].ToolCalls[0].Input); got != `{"a":1}` {
		d.t.Fatalf("expected original history tool input, got %q", got)
	}
	if got := in.History[0].ToolCalls[0].Caller.Type; got != "original" {
		d.t.Fatalf("expected original caller type, got %q", got)
	}
	if got := string(in.History[1].ToolResults[0].Output); got != `{"ok":true}` {
		d.t.Fatalf("expected original history tool output, got %q", got)
	}
	if got := in.History[1].ToolResults[0].InlineData[0].Data[0]; got != 1 {
		d.t.Fatalf("expected original inline data, got %d", got)
	}
	if got := in.History[1].ToolResults[0].Error.Message; got != "original" {
		d.t.Fatalf("expected original tool error, got %q", got)
	}
	if got := string(in.Tools[0].InputSchema); got != `{"type":"object"}` {
		d.t.Fatalf("expected original tool schema, got %q", got)
	}
	if got := string(in.Tools[0].Examples[0].Input); got != `{"input":true}` {
		d.t.Fatalf("expected original tool example input, got %q", got)
	}
	if got := string(in.Tools[0].Examples[0].Output); got != `{"output":true}` {
		d.t.Fatalf("expected original tool example output, got %q", got)
	}
	if got := string(in.ToolCalls[0].Input); got != `{"a":1}` {
		d.t.Fatalf("expected original extracted tool input, got %q", got)
	}
	if got := string(in.ToolResults[0].Output); got != `{"ok":true}` {
		d.t.Fatalf("expected original extracted tool output, got %q", got)
	}
	return Decision{Reply: "done"}, nil
}

type schemaExecutor struct{}

func (schemaExecutor) ListTools(context.Context) ([]agentic.ToolDefinition, error) {
	return []agentic.ToolDefinition{{
		Name:        "schema",
		Description: "schema tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Examples: []agentic.ToolExample{{
			Input:  json.RawMessage(`{"input":true}`),
			Output: json.RawMessage(`{"output":true}`),
		}},
	}}, nil
}

func (schemaExecutor) Execute(context.Context, agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{}, nil
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

type historyMergeDecider struct {
	t *testing.T
}

func (d *historyMergeDecider) Decide(_ context.Context, in Input) (Decision, error) {
	hasStore := false
	hasRequest := false
	for _, msg := range in.History {
		if msg.Content == "from-store" {
			hasStore = true
		}
		if msg.Content == "from-request" {
			hasRequest = true
		}
	}
	if !hasStore || !hasRequest {
		d.t.Fatalf("expected merged history from store+request, got %#v", in.History)
	}
	return Decision{Reply: "done"}, nil
}

type failingAppendStore struct {
	err      error
	messages []message.AgentMessage
}

func (s *failingAppendStore) Append(_ context.Context, _ message.AgentMessage) error {
	return s.err
}

func (s *failingAppendStore) Load(_ context.Context) ([]message.AgentMessage, error) {
	out := make([]message.AgentMessage, len(s.messages))
	copy(out, s.messages)
	return out, nil
}

type failingReplaceStore struct {
	err      error
	messages []message.AgentMessage
}

func (s *failingReplaceStore) Append(_ context.Context, msg message.AgentMessage) error {
	s.messages = append(s.messages, msg)
	return nil
}

func (s *failingReplaceStore) Load(_ context.Context) ([]message.AgentMessage, error) {
	out := make([]message.AgentMessage, len(s.messages))
	copy(out, s.messages)
	return out, nil
}

func (s *failingReplaceStore) Replace(_ context.Context, _ []message.AgentMessage) error {
	return s.err
}

// usageTrackingDecider returns per-step usage and completes after one tool call.
type usageTrackingDecider struct {
	calls int
}

func (d *usageTrackingDecider) Decide(_ context.Context, _ Input) (Decision, error) {
	d.calls++
	switch d.calls {
	case 1:
		// First call: tool call with cache write (new cache)
		return Decision{
			ToolCalls: []agentic.ToolCall{{
				Name:  "echo",
				Input: json.RawMessage(`{"text":"step1"}`),
			}},
			Usage: &usage.Usage{
				Input: 50, Output: 10,
				CacheReadInput: 0, CacheCreationInput: 500,
			},
		}, nil
	case 2:
		// Second call: tool call with cache hit
		return Decision{
			ToolCalls: []agentic.ToolCall{{
				Name:  "echo",
				Input: json.RawMessage(`{"text":"step2"}`),
			}},
			Usage: &usage.Usage{
				Input: 5, Output: 15,
				CacheReadInput: 500, CacheCreationInput: 10,
			},
		}, nil
	default:
		// Final reply with cache hit
		return Decision{
			Reply: "done",
			Usage: &usage.Usage{
				Input: 3, Output: 20,
				CacheReadInput: 510, CacheCreationInput: 5,
			},
		}, nil
	}
}

func TestRunAggregatesUsageAcrossSteps(t *testing.T) {
	runner := New(Config{
		Decider:  &usageTrackingDecider{},
		Executor: stubExecutor{},
		MaxTurns: 10,
	})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "done" {
		t.Fatalf("expected reply 'done', got %q", result.Reply)
	}

	// Verify Steps count: 3 Decide() calls (tool, tool, reply)
	if result.Steps != 3 {
		t.Fatalf("expected Steps=3, got %d", result.Steps)
	}

	// Verify aggregated usage: sum of all 3 steps
	u := result.Usage
	if u == nil {
		t.Fatal("expected non-nil Usage")
	}
	if u.Input != 58 {
		t.Fatalf("expected aggregated Input=58, got %d", u.Input)
	}
	if u.Output != 45 {
		t.Fatalf("expected aggregated Output=45, got %d", u.Output)
	}
	if u.CacheReadInput != 1010 {
		t.Fatalf("expected aggregated CacheReadInput=1010, got %d", u.CacheReadInput)
	}
	if u.CacheCreationInput != 515 {
		t.Fatalf("expected aggregated CacheCreationInput=515, got %d", u.CacheCreationInput)
	}
	if u.Total != 103 {
		t.Fatalf("expected aggregated Total=103, got %d", u.Total)
	}
}

func TestRunSingleStepUsage(t *testing.T) {
	// A single-step reply (no tool calls) should still return correct usage.
	decider := &replyDecider{reply: "hello"}
	runner := New(Config{Decider: decider})

	result, err := runner.Run(context.Background(), Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Steps != 1 {
		t.Fatalf("expected Steps=1, got %d", result.Steps)
	}
	// Usage comes from the decider which returns nil, so aggregated is zero-value.
	if result.Usage == nil {
		t.Fatal("expected non-nil Usage")
	}
}
