package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/message"
)

func TestLoopInlineDataPreservedInHistory(t *testing.T) {
	imageBytes := []byte("fake-png-image-bytes")

	executor := executorFunc{
		listFn: func(ctx context.Context) ([]agentic.ToolDefinition, error) {
			return []agentic.ToolDefinition{{Name: "generate_image"}}, nil
		},
		execFn: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			return agentic.ToolResult{
				Name:   call.Name,
				Output: json.RawMessage(`{"ref":"img-001"}`),
				InlineData: []agentic.InlineData{
					{MIMEType: "image/png", Data: imageBytes},
				},
			}, nil
		},
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "generate_image", Input: json.RawMessage(`{"prompt":"a cat"}`)}}},
			{Reply: "done"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: executor,
	}, loop.Request{UserMessage: "generate something"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the tool result in the final result has inline data
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(result.ToolResults))
	}
	if len(result.ToolResults[0].InlineData) != 1 {
		t.Fatalf("expected 1 inline data in tool result, got %d", len(result.ToolResults[0].InlineData))
	}
	if result.ToolResults[0].InlineData[0].MIMEType != "image/png" {
		t.Errorf("expected mime type image/png, got %q", result.ToolResults[0].InlineData[0].MIMEType)
	}
	if string(result.ToolResults[0].InlineData[0].Data) != string(imageBytes) {
		t.Errorf("inline data bytes mismatch")
	}

	// Verify the tool result in history also has inline data
	var toolMsg *message.AgentMessage
	for i := range result.History {
		if result.History[i].Role == message.RoleTool {
			toolMsg = &result.History[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("expected a tool message in history")
	}
	if len(toolMsg.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result in history message, got %d", len(toolMsg.ToolResults))
	}
	if len(toolMsg.ToolResults[0].InlineData) != 1 {
		t.Fatal("expected inline data preserved in history tool result")
	}
	if toolMsg.ToolResults[0].InlineData[0].MIMEType != "image/png" {
		t.Errorf("history inline data mime type mismatch: %q", toolMsg.ToolResults[0].InlineData[0].MIMEType)
	}

	// Verify the decider received the inline data in its second input (after tool execution)
	if len(decider.inputs) < 2 {
		t.Fatalf("expected at least 2 decider inputs, got %d", len(decider.inputs))
	}
	secondInput := decider.inputs[1]
	var foundInlineData bool
	for _, msg := range secondInput.History {
		if msg.Role == message.RoleTool {
			for _, tr := range msg.ToolResults {
				if len(tr.InlineData) > 0 {
					foundInlineData = true
				}
			}
		}
	}
	if !foundInlineData {
		t.Error("decider did not receive inline data in history on second turn")
	}
}

func TestLoopInlineDataPreservedInSessionStore(t *testing.T) {
	imageBytes := []byte("stored-image")
	store := &appendOnlyStore{}

	executor := executorFunc{
		listFn: func(ctx context.Context) ([]agentic.ToolDefinition, error) {
			return []agentic.ToolDefinition{{Name: "gen"}}, nil
		},
		execFn: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			return agentic.ToolResult{
				Name:   call.Name,
				Output: json.RawMessage(`"ok"`),
				InlineData: []agentic.InlineData{
					{MIMEType: "image/jpeg", Data: imageBytes},
				},
			}, nil
		},
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "gen", Input: json.RawMessage(`{}`)}}},
			{Reply: "done"},
		},
	}

	_, _, err := runScenario(t, loop.Config{
		Decider:      decider,
		Executor:     executor,
		HistoryStore: store,
	}, loop.Request{UserMessage: "go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the tool result in the persisted store
	var found bool
	for _, msg := range store.messages {
		if msg.Role == message.RoleTool {
			for _, tr := range msg.ToolResults {
				if len(tr.InlineData) > 0 && tr.InlineData[0].MIMEType == "image/jpeg" {
					if string(tr.InlineData[0].Data) == string(imageBytes) {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Error("inline data not preserved in session store")
	}
}
