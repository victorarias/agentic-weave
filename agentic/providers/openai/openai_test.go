package openai

import (
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/providers"
)

func TestBuildMessages_SystemPrompt(t *testing.T) {
	msgs := buildMessages("You are helpful.", nil, "Hello")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestBuildMessages_History(t *testing.T) {
	history := []message.AgentMessage{
		{Role: message.RoleUser, Content: "Hi"},
		{Role: message.RoleAssistant, Content: "Hello!"},
		{Role: message.RoleTool, ToolResults: []agentic.ToolResult{{ID: "tc1", Output: []byte(`"ok"`)}}},
		{Role: message.RoleSystem, Content: "Summary of conversation so far."},
	}
	msgs := buildMessages("system", history, "Follow up")
	// system + 4 history + 1 user = 6
	if len(msgs) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(msgs))
	}
}

func TestBuildMessages_AssistantWithToolCalls(t *testing.T) {
	history := []message.AgentMessage{
		{Role: message.RoleUser, Content: "What's the weather?"},
		{
			Role:    message.RoleAssistant,
			Content: "Let me check.",
			ToolCalls: []agentic.ToolCall{
				{ID: "tc1", Name: "get_weather", Input: []byte(`{"city":"NYC"}`)},
			},
		},
		{Role: message.RoleTool, ToolResults: []agentic.ToolResult{{ID: "tc1", Output: []byte(`"Sunny"`)}}},
	}
	msgs := buildMessages("", history, "Thanks")
	// 3 history + 1 user = 4
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
}

func TestToolDefsToOpenAI(t *testing.T) {
	tools := []agentic.ToolDefinition{
		{
			Name:        "search",
			Description: "Search the web",
			InputSchema: []byte(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		},
	}
	out := toolDefsToOpenAI(tools)
	if len(out) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(out))
	}
}

func TestNormalizeStopReason(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"stop", "end_turn"},
		{"tool_calls", "tool_use"},
		{"length", "max_tokens"},
		{"other", "other"},
	}
	for _, tc := range tests {
		got := normalizeStopReason(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeStopReason(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestNormalizeReasoningEffort(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"high", "high"},
		{"xhigh", "xhigh"},
		{"medium", "medium"},
		{"", ""},
		{"unknown", ""},
	}
	for _, tc := range tests {
		got := string(normalizeReasoningEffort(tc.input))
		if got != tc.expected {
			t.Errorf("normalizeReasoningEffort(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// Verify interface compliance at compile time.
var _ providers.Streamer = (*Client)(nil)
