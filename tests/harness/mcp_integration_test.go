package harness

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/mcp"
)

type mcpClientStub struct {
	tools []agentic.ToolDefinition
	exec  map[string]agentic.ToolResult
	err   error
}

func (c *mcpClientStub) ListTools(ctx context.Context) ([]agentic.ToolDefinition, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.tools, nil
}

func (c *mcpClientStub) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if c.err != nil {
		return agentic.ToolResult{}, c.err
	}
	res, ok := c.exec[call.Name]
	if !ok {
		return agentic.ToolResult{}, agentic.ErrToolNotFound
	}
	return res, nil
}

func TestMCPRegistryFiltersToolsInLoop(t *testing.T) {
	client := &mcpClientStub{
		tools: []agentic.ToolDefinition{
			{Name: "allowed"},
			{Name: "blocked"},
		},
	}
	reg := mcp.NewRegistry(client, []string{"allowed"})
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
	if len(decider.inputs) == 0 || len(decider.inputs[0].Tools) != 1 {
		t.Fatalf("unexpected tools list: %#v", decider.inputs)
	}
	if decider.inputs[0].Tools[0].Name != "allowed" {
		t.Fatalf("expected only allowed tool, got %q", decider.inputs[0].Tools[0].Name)
	}
}

func TestMCPRegistryBlocksExecution(t *testing.T) {
	client := &mcpClientStub{
		tools: []agentic.ToolDefinition{{Name: "allowed"}, {Name: "blocked"}},
		exec: map[string]agentic.ToolResult{
			"allowed": {Name: "allowed", Output: []byte("ok")},
		},
	}
	reg := mcp.NewRegistry(client, []string{"allowed"})
	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "blocked", Input: []byte(`{}`)}}},
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
	if !strings.Contains(result.ToolResults[0].Error.Message, "tool not found") {
		t.Fatalf("unexpected error message: %q", result.ToolResults[0].Error.Message)
	}
}

func TestMCPRegistryPropagatesClientError(t *testing.T) {
	client := &mcpClientStub{err: errors.New("boom")}
	reg := mcp.NewRegistry(client, nil)
	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: []byte(`{}`)}}},
			{Reply: "done"},
		},
	}

	_, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: reg,
	}, loop.Request{UserMessage: "hi"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}
