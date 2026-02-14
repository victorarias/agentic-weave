package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/message"
)

func TestLoopToolDefaults(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(staticTool{
		def: agentic.ToolDefinition{
			Name:           "echo",
			Description:    "echo",
			AllowedCallers: []string{"llm"},
		},
		output: json.RawMessage(`"ok"`),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
			{Reply: "done"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: reg,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call")
	}
	if !strings.HasPrefix(result.ToolCalls[0].ID, "call-") {
		t.Fatalf("unexpected tool call id: %q", result.ToolCalls[0].ID)
	}
	if result.ToolCalls[0].Caller == nil || result.ToolCalls[0].Caller.Type != "llm" {
		t.Fatalf("unexpected tool caller: %#v", result.ToolCalls[0].Caller)
	}
}

func TestLoopToolCallerTypeOverride(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(staticTool{
		def: agentic.ToolDefinition{
			Name:           "echo",
			Description:    "echo",
			AllowedCallers: []string{"programmatic"},
		},
		output: json.RawMessage(`"ok"`),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
			{Reply: "done"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:        decider,
		Executor:       reg,
		ToolCallerType: "programmatic",
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ToolCalls[0].Caller == nil || result.ToolCalls[0].Caller.Type != "programmatic" {
		t.Fatalf("unexpected tool caller: %#v", result.ToolCalls[0].Caller)
	}
}

func TestLoopToolMissingExecutor(t *testing.T) {
	decider := &scriptedDecider{
		script: []loop.Decision{{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}}},
	}
	_, _, err := runScenario(t, loop.Config{
		Decider: decider,
	}, loop.Request{UserMessage: "hi"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "no executor") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoopToolExecutorError(t *testing.T) {
	executor := executorFunc{
		listFn: func(ctx context.Context) ([]agentic.ToolDefinition, error) {
			return []agentic.ToolDefinition{{Name: "echo"}}, nil
		},
		execFn: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			return agentic.ToolResult{}, errors.New("boom")
		},
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
			{Reply: "done"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: executor,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Error == nil {
		t.Fatalf("expected tool error")
	}
	if !strings.Contains(result.ToolResults[0].Error.Message, "boom") {
		t.Fatalf("unexpected error message: %q", result.ToolResults[0].Error.Message)
	}
}

func TestLoopToolCallDefaultsPersistInHistory(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(staticTool{
		def: agentic.ToolDefinition{
			Name:           "echo",
			Description:    "echo",
			AllowedCallers: []string{"llm"},
		},
		output: json.RawMessage(`"ok"`),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
			{Reply: "done"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: reg,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var toolMsg *message.AgentMessage
	for i := range result.History {
		msg := &result.History[i]
		if msg.Role == message.RoleAssistant && len(msg.ToolCalls) > 0 {
			toolMsg = msg
			break
		}
	}
	if toolMsg == nil {
		t.Fatalf("expected assistant tool call message in history")
	}
	if !strings.HasPrefix(toolMsg.ToolCalls[0].ID, "call-") {
		t.Fatalf("expected tool call id in history, got %q", toolMsg.ToolCalls[0].ID)
	}
	if toolMsg.ToolCalls[0].Caller == nil || toolMsg.ToolCalls[0].Caller.Type != "llm" {
		t.Fatalf("expected tool caller in history, got %#v", toolMsg.ToolCalls[0].Caller)
	}
}

func TestLoopToolSchemaMismatch(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(staticTool{
		def: agentic.ToolDefinition{
			Name:           "echo",
			SchemaHash:     "abc",
			AllowedCallers: []string{"llm"},
		},
		output: json.RawMessage(`"ok"`),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", SchemaHash: "def", Input: json.RawMessage(`{}`)}}},
			{Reply: "done"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: reg,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Error == nil {
		t.Fatalf("expected tool error")
	}
	if !strings.Contains(result.ToolResults[0].Error.Message, "schema mismatch") {
		t.Fatalf("unexpected error message: %q", result.ToolResults[0].Error.Message)
	}
}

func TestLoopToolCallerDenied(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(staticTool{
		def: agentic.ToolDefinition{
			Name:           "echo",
			AllowedCallers: []string{"programmatic"},
		},
		output: json.RawMessage(`"ok"`),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
			{Reply: "done"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: reg,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Error == nil {
		t.Fatalf("expected tool error")
	}
	if !strings.Contains(result.ToolResults[0].Error.Message, "caller not allowed") {
		t.Fatalf("unexpected error message: %q", result.ToolResults[0].Error.Message)
	}
}

func TestLoopMaxTurnsStops(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(staticTool{
		def: agentic.ToolDefinition{
			Name:           "echo",
			AllowedCallers: []string{"llm"},
		},
		output: json.RawMessage(`"ok"`),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: reg,
		MaxTurns: 1,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolCalls) != 2 || len(result.ToolResults) != 1 {
		t.Fatalf("expected 2 tool calls (executed+pending) and 1 result, got calls=%d results=%d", len(result.ToolCalls), len(result.ToolResults))
	}
	// Loop passes through decider's reply without adding defaults
}

func TestLoopToolListingRespectsPolicy(t *testing.T) {
	reg := agentic.NewRegistry(agentic.WithPolicy(agentic.NewAllowlistPolicy([]string{"allowed"})))
	if err := reg.Register(
		staticTool{def: agentic.ToolDefinition{Name: "allowed"}},
		staticTool{def: agentic.ToolDefinition{Name: "blocked"}},
	); err != nil {
		t.Fatalf("register tools: %v", err)
	}

	decider := &scriptedDecider{
		script: []loop.Decision{{Reply: "ok"}},
	}

	_, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: reg,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decider.inputs) == 0 {
		t.Fatalf("expected decider input")
	}
	if len(decider.inputs[0].Tools) != 1 || decider.inputs[0].Tools[0].Name != "allowed" {
		t.Fatalf("unexpected tools list: %#v", decider.inputs[0].Tools)
	}
}
