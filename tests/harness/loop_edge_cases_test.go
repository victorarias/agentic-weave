package harness

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/history"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/message"
)

// --- Error Recovery Tests ---

type failingCompactor struct {
	callCount int
	failAfter int
}

func (c *failingCompactor) Compact(ctx context.Context, messages []budget.Budgetable) (string, error) {
	c.callCount++
	if c.callCount > c.failAfter {
		return "", errors.New("compaction failed")
	}
	return "summary", nil
}

func TestLoopCompactionFailure(t *testing.T) {
	store := history.NewMemoryStore()
	// Add enough history to trigger compaction
	_ = store.Append(context.Background(), message.AgentMessage{Role: message.RoleUser, Content: "hello"})
	_ = store.Append(context.Background(), message.AgentMessage{Role: message.RoleAssistant, Content: "world"})

	compactor := &failingCompactor{failAfter: 0}
	budgetMgr := &budget.Manager{
		Counter:   budget.CharCounter{},
		Compactor: compactor,
		Policy: budget.Policy{
			ContextWindow: 1, // Force compaction
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
		t.Fatalf("expected compaction error")
	}
	if err.Error() != "compaction failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Boundary Condition Tests ---

func TestLoopEmptyHistory(t *testing.T) {
	decider := &scriptedDecider{
		script: []loop.Decision{{Reply: "hello from empty"}},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider: decider,
	}, loop.Request{
		UserMessage: "hi",
		History:     []message.AgentMessage{}, // Explicitly empty
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "hello from empty" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
	// History should contain: user message + assistant reply
	if len(result.History) != 2 {
		t.Fatalf("expected 2 history messages, got %d", len(result.History))
	}
}

func TestLoopSingleMessageHistory(t *testing.T) {
	decider := &scriptedDecider{
		script: []loop.Decision{{Reply: "response"}},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider: decider,
	}, loop.Request{
		UserMessage: "follow-up",
		History: []message.AgentMessage{
			{Role: message.RoleSystem, Content: "you are helpful"},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decider.inputs) != 1 {
		t.Fatalf("expected 1 decider call")
	}
	// Should see: system message + user message
	if len(decider.inputs[0].History) != 2 {
		t.Fatalf("expected 2 messages in decider history, got %d", len(decider.inputs[0].History))
	}
	if result.Reply != "response" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
}

func TestLoopEmptyUserMessage(t *testing.T) {
	decider := &scriptedDecider{
		script: []loop.Decision{{Reply: "what do you need?"}},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider: decider,
	}, loop.Request{
		UserMessage: "", // Empty user message
		History: []message.AgentMessage{
			{Role: message.RoleUser, Content: "previous question"},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty user message should not be added
	if len(decider.inputs[0].History) != 1 {
		t.Fatalf("expected 1 message in history (no empty user message), got %d", len(decider.inputs[0].History))
	}
	if result.Reply != "what do you need?" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
}

// --- Complex Tool Interaction Tests ---

func TestLoopMultipleToolsInSequence(t *testing.T) {
	callOrder := []string{}
	executor := executorFunc{
		listFn: func(ctx context.Context) ([]agentic.ToolDefinition, error) {
			return []agentic.ToolDefinition{
				{Name: "first_tool"},
				{Name: "second_tool"},
			}, nil
		},
		execFn: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			callOrder = append(callOrder, call.Name)
			return agentic.ToolResult{
				ID:     call.ID,
				Name:   call.Name,
				Output: json.RawMessage(`"done"`),
			}, nil
		},
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			// First turn: call first_tool
			{ToolCalls: []agentic.ToolCall{{Name: "first_tool", Input: json.RawMessage(`{}`)}}},
			// Second turn: call second_tool
			{ToolCalls: []agentic.ToolCall{{Name: "second_tool", Input: json.RawMessage(`{}`)}}},
			// Final turn: reply
			{Reply: "all tools executed"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: executor,
		MaxTurns: 5,
	}, loop.Request{UserMessage: "run tools"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(callOrder) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(callOrder))
	}
	if callOrder[0] != "first_tool" || callOrder[1] != "second_tool" {
		t.Fatalf("unexpected call order: %v", callOrder)
	}
	if result.Reply != "all tools executed" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
}

func TestLoopToolFailureMidSequence(t *testing.T) {
	callCount := 0
	executor := executorFunc{
		listFn: func(ctx context.Context) ([]agentic.ToolDefinition, error) {
			return []agentic.ToolDefinition{
				{Name: "working_tool"},
				{Name: "failing_tool"},
			}, nil
		},
		execFn: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			callCount++
			if call.Name == "failing_tool" {
				return agentic.ToolResult{}, errors.New("tool execution failed")
			}
			return agentic.ToolResult{
				ID:     call.ID,
				Name:   call.Name,
				Output: json.RawMessage(`"success"`),
			}, nil
		},
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			// Call working_tool first
			{ToolCalls: []agentic.ToolCall{{Name: "working_tool", Input: json.RawMessage(`{}`)}}},
			// Then failing_tool
			{ToolCalls: []agentic.ToolCall{{Name: "failing_tool", Input: json.RawMessage(`{}`)}}},
			// Should still get here with error in result
			{Reply: "handled the failure"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: executor,
		MaxTurns: 5,
	}, loop.Request{UserMessage: "test failure handling"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 tool calls, got %d", callCount)
	}
	// Check that the error was captured in tool results
	if len(result.ToolResults) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(result.ToolResults))
	}
	// Second result should have an error
	if result.ToolResults[1].Error == nil {
		t.Fatalf("expected error in second tool result")
	}
	if result.ToolResults[1].Error.Message != "tool execution failed" {
		t.Fatalf("unexpected error message: %q", result.ToolResults[1].Error.Message)
	}
	if result.Reply != "handled the failure" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
}

func TestLoopParallelToolCalls(t *testing.T) {
	calledTools := []string{}
	executor := executorFunc{
		listFn: func(ctx context.Context) ([]agentic.ToolDefinition, error) {
			return []agentic.ToolDefinition{
				{Name: "tool_a"},
				{Name: "tool_b"},
				{Name: "tool_c"},
			}, nil
		},
		execFn: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			calledTools = append(calledTools, call.Name)
			return agentic.ToolResult{
				ID:     call.ID,
				Name:   call.Name,
				Output: json.RawMessage(`"` + call.Name + ` result"`),
			}, nil
		},
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			// Single turn with multiple parallel tool calls
			{ToolCalls: []agentic.ToolCall{
				{Name: "tool_a", Input: json.RawMessage(`{}`)},
				{Name: "tool_b", Input: json.RawMessage(`{}`)},
				{Name: "tool_c", Input: json.RawMessage(`{}`)},
			}},
			// Reply after all tools complete
			{Reply: "all parallel tools done"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: executor,
		MaxTurns: 3,
	}, loop.Request{UserMessage: "run parallel"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calledTools) != 3 {
		t.Fatalf("expected 3 tool calls, got %d", len(calledTools))
	}
	if len(result.ToolResults) != 3 {
		t.Fatalf("expected 3 tool results, got %d", len(result.ToolResults))
	}
	if result.Reply != "all parallel tools done" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
}

func TestLoopMaxTurnsReachedWithPendingTools(t *testing.T) {
	executor := executorFunc{
		listFn: func(ctx context.Context) ([]agentic.ToolDefinition, error) {
			return []agentic.ToolDefinition{{Name: "infinite_tool"}}, nil
		},
		execFn: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			return agentic.ToolResult{
				ID:     call.ID,
				Name:   call.Name,
				Output: json.RawMessage(`"keep going"`),
			}, nil
		},
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			// Keep calling tools forever
			{ToolCalls: []agentic.ToolCall{{Name: "infinite_tool", Input: json.RawMessage(`{}`)}}},
			{ToolCalls: []agentic.ToolCall{{Name: "infinite_tool", Input: json.RawMessage(`{}`)}}},
			{ToolCalls: []agentic.ToolCall{{Name: "infinite_tool", Input: json.RawMessage(`{}`)}}},
			{ToolCalls: []agentic.ToolCall{{Name: "infinite_tool", Input: json.RawMessage(`{}`)}}},
		},
	}

	_, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: executor,
		MaxTurns: 2, // Limit to 2 turns
	}, loop.Request{UserMessage: "loop forever"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Loop passes through decider's reply without adding defaults
	// Decider should have been called 3 times (turn 0, 1, 2 but exits at turn 2)
	if len(decider.inputs) != 3 {
		t.Fatalf("expected 3 decider calls (0, 1, exit at 2), got %d", len(decider.inputs))
	}
}
