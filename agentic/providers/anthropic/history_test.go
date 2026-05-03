package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
)

func TestAppendHistoryToolResultWithInlineData(t *testing.T) {
	imageBytes := []byte("fake-png-data")
	history := []message.AgentMessage{
		{
			Role: message.RoleAssistant,
			ToolCalls: []agentic.ToolCall{
				{ID: "call-img", Name: "evaluate_image", Input: json.RawMessage(`{"ref":"img-1"}`)},
			},
		},
		{
			Role: message.RoleTool,
			ToolResults: []agentic.ToolResult{
				{
					ID:     "call-img",
					Name:   "evaluate_image",
					Output: json.RawMessage(`"Here is the image"`),
					InlineData: []agentic.InlineData{
						{MIMEType: "image/png", Data: imageBytes},
					},
				},
			},
		},
	}

	messages := appendHistory(nil, history)

	// Should have 2 messages: assistant (tool_use) + user (tool_result)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// Serialize the tool result message and verify it contains image data
	raw, err := json.Marshal(messages[1])
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	rawStr := string(raw)
	// Should contain image content and tool use ID in the tool result
	if !contains(rawStr, "image/png") {
		t.Errorf("expected image/png media type in serialized output, got: %s", rawStr)
	}
	if !contains(rawStr, "call-img") {
		t.Errorf("expected tool_use_id in output, got: %s", rawStr)
	}
	if !contains(rawStr, "Here is the image") {
		t.Errorf("expected successful tool result text in output, got: %s", rawStr)
	}
	if !contains(rawStr, "base64") {
		t.Errorf("expected base64 source type in output, got: %s", rawStr)
	}
}

func TestAppendHistoryToolResultWithInlineDataAndEmptyOutputKeepsTextPart(t *testing.T) {
	history := []message.AgentMessage{
		{
			Role: message.RoleAssistant,
			ToolCalls: []agentic.ToolCall{
				{ID: "call-img-empty", Name: "evaluate_image", Input: json.RawMessage(`{"ref":"img-1"}`)},
			},
		},
		{
			Role: message.RoleTool,
			ToolResults: []agentic.ToolResult{
				{
					ID:   "call-img-empty",
					Name: "evaluate_image",
					InlineData: []agentic.InlineData{
						{MIMEType: "image/png", Data: []byte("fake-png-data")},
					},
				},
			},
		},
	}

	messages := appendHistory(nil, history)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	raw, err := json.Marshal(messages[1])
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	rawStr := string(raw)
	if !contains(rawStr, `"text":"null"`) {
		t.Errorf("expected fallback null text part in output, got: %s", rawStr)
	}
	if !contains(rawStr, "image/png") {
		t.Errorf("expected image/png media type in serialized output, got: %s", rawStr)
	}
}

func TestAppendHistoryToolResultWithoutInlineData(t *testing.T) {
	history := []message.AgentMessage{
		{
			Role: message.RoleAssistant,
			ToolCalls: []agentic.ToolCall{
				{ID: "call-text", Name: "search", Input: json.RawMessage(`{}`)},
			},
		},
		{
			Role: message.RoleTool,
			ToolResults: []agentic.ToolResult{
				{ID: "call-text", Name: "search", Output: json.RawMessage(`"found results"`)},
			},
		},
	}

	messages := appendHistory(nil, history)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// Should serialize without error
	_, err := json.Marshal(messages[1])
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
}

func TestAppendHistoryUserInlineData(t *testing.T) {
	history := []message.AgentMessage{
		{
			Role:       message.RoleUser,
			Content:    "Describe this image",
			InlineData: []agentic.InlineData{{MIMEType: "image/png", Data: []byte("fake-png-data")}},
		},
	}

	messages := appendHistory(nil, history)

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	raw, err := json.Marshal(messages[0])
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	rawStr := string(raw)
	if !contains(rawStr, "Describe this image") {
		t.Errorf("expected text content in serialized output, got: %s", rawStr)
	}
	if !contains(rawStr, "image/png") {
		t.Errorf("expected image/png media type in serialized output, got: %s", rawStr)
	}
	if !contains(rawStr, "base64") {
		t.Errorf("expected base64 source type in output, got: %s", rawStr)
	}
}

func TestUserMessageParamAllowsImageOnly(t *testing.T) {
	msg, ok := userMessageParam("", []agentic.InlineData{{MIMEType: "image/jpeg", Data: []byte("jpg")}})
	if !ok {
		t.Fatal("expected image-only user message")
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !contains(string(raw), "image/jpeg") {
		t.Fatalf("expected image/jpeg payload, got %s", string(raw))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
