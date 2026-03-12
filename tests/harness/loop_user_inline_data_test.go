package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/message"
)

func TestLoopUserInlineDataPassedToDecider(t *testing.T) {
	imageData := []byte("fake-screenshot-bytes")
	inlineData := []agentic.InlineData{
		{MIMEType: "image/jpeg", Data: imageData},
	}

	// A tool that returns a simple result — we use it to force a second Decide() call.
	executor := executorFunc{
		listFn: func(ctx context.Context) ([]agentic.ToolDefinition, error) {
			return []agentic.ToolDefinition{{Name: "echo"}}, nil
		},
		execFn: func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
			return agentic.ToolResult{
				Name:   call.Name,
				Output: json.RawMessage(`"ok"`),
			}, nil
		},
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			// Turn 0: call a tool
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
			// Turn 1: reply
			{Reply: "I see the image"},
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider:  decider,
		Executor: executor,
	}, loop.Request{
		UserMessage:    "what's in this screenshot?",
		UserInlineData: inlineData,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify: first Decide() receives UserInlineData
	if len(decider.inputs) < 2 {
		t.Fatalf("expected at least 2 decider inputs, got %d", len(decider.inputs))
	}
	firstInput := decider.inputs[0]
	if len(firstInput.UserInlineData) != 1 {
		t.Fatalf("expected 1 inline data on first Decide(), got %d", len(firstInput.UserInlineData))
	}
	if firstInput.UserInlineData[0].MIMEType != "image/jpeg" {
		t.Errorf("expected mime type image/jpeg on first Decide(), got %q", firstInput.UserInlineData[0].MIMEType)
	}
	if string(firstInput.UserInlineData[0].Data) != string(imageData) {
		t.Error("inline data bytes mismatch on first Decide()")
	}

	// Verify: second Decide() does NOT receive UserInlineData (it's already in history)
	secondInput := decider.inputs[1]
	if len(secondInput.UserInlineData) != 0 {
		t.Errorf("expected no inline data on second Decide(), got %d", len(secondInput.UserInlineData))
	}

	// Verify: inline data is preserved in the user message in history
	var userMsg *message.AgentMessage
	for i := range result.History {
		if result.History[i].Role == message.RoleUser {
			userMsg = &result.History[i]
			break
		}
	}
	if userMsg == nil {
		t.Fatal("expected a user message in history")
	}
	if len(userMsg.InlineData) != 1 {
		t.Fatalf("expected 1 inline data in history user message, got %d", len(userMsg.InlineData))
	}
	if userMsg.InlineData[0].MIMEType != "image/jpeg" {
		t.Errorf("history inline data mime type mismatch: %q", userMsg.InlineData[0].MIMEType)
	}

	// Verify: the reply came through
	if result.Reply != "I see the image" {
		t.Errorf("unexpected reply: %q", result.Reply)
	}
}

func TestLoopUserInlineDataInHistoryReachesDecider(t *testing.T) {
	imageData := []byte("history-image")

	decider := &scriptedDecider{
		script: []loop.Decision{
			{Reply: "I can see it"},
		},
	}

	// Inline data appears in history (from a previous turn's user message).
	history := []message.AgentMessage{
		{
			Role:    message.RoleUser,
			Content: "here's an image",
			InlineData: []agentic.InlineData{
				{MIMEType: "image/png", Data: imageData},
			},
		},
		{
			Role:    message.RoleAssistant,
			Content: "interesting, tell me more",
		},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider: decider,
	}, loop.Request{
		UserMessage: "what do you think?",
		History:     history,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify: decider received the history with inline data
	if len(decider.inputs) != 1 {
		t.Fatalf("expected 1 decider input, got %d", len(decider.inputs))
	}
	input := decider.inputs[0]

	// History should contain: original user msg (with inline data), assistant msg, new user msg
	var foundInlineData bool
	for _, msg := range input.History {
		if msg.Role == message.RoleUser && len(msg.InlineData) > 0 {
			if msg.InlineData[0].MIMEType == "image/png" {
				foundInlineData = true
			}
		}
	}
	if !foundInlineData {
		t.Error("decider did not receive inline data from history")
	}

	if result.Reply != "I can see it" {
		t.Errorf("unexpected reply: %q", result.Reply)
	}
}
