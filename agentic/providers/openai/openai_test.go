package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/providers"
)

func TestBuildMessages_SystemPrompt(t *testing.T) {
	msgs, reasonings := buildMessages("You are helpful.", nil, "Hello", nil)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if len(reasonings) != len(msgs) {
		t.Fatalf("reasonings slice must align with messages: got %d vs %d", len(reasonings), len(msgs))
	}
}

func TestBuildMessages_History(t *testing.T) {
	history := []message.AgentMessage{
		{Role: message.RoleUser, Content: "Hi"},
		{Role: message.RoleAssistant, Content: "Hello!"},
		{Role: message.RoleTool, ToolResults: []agentic.ToolResult{{ID: "tc1", Output: []byte(`"ok"`)}}},
		{Role: message.RoleSystem, Content: "Summary of conversation so far."},
	}
	msgs, reasonings := buildMessages("system", history, "Follow up", nil)
	// system + 4 history + 1 user = 6
	if len(msgs) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(msgs))
	}
	if len(reasonings) != len(msgs) {
		t.Fatalf("reasonings slice must align with messages: got %d vs %d", len(reasonings), len(msgs))
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
	msgs, reasonings := buildMessages("", history, "Thanks", nil)
	// 3 history + 1 user = 4
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if len(reasonings) != len(msgs) {
		t.Fatalf("reasonings slice must align with messages: got %d vs %d", len(reasonings), len(msgs))
	}
}

func TestBuildMessages_ReasoningContentAlignedToAssistantSlot(t *testing.T) {
	history := []message.AgentMessage{
		{Role: message.RoleUser, Content: "Solve 2+2"},
		{Role: message.RoleAssistant, Content: "4", ReasoningContent: "two plus two is four"},
		{Role: message.RoleUser, Content: "Now 3+3"},
		{Role: message.RoleAssistant, Content: "6"},
	}
	msgs, reasonings := buildMessages("", history, "again?", nil)
	if len(msgs) != len(reasonings) {
		t.Fatalf("misaligned: %d msgs vs %d reasonings", len(msgs), len(reasonings))
	}
	for i, m := range msgs {
		if m.OfAssistant == nil {
			if reasonings[i] != "" {
				t.Errorf("non-assistant slot %d carries reasoning %q", i, reasonings[i])
			}
			continue
		}
	}
	// Two assistant slots, one with reasoning and one without.
	var withReasoning, withoutReasoning int
	for i, m := range msgs {
		if m.OfAssistant == nil {
			continue
		}
		if reasonings[i] != "" {
			withReasoning++
			if reasonings[i] != "two plus two is four" {
				t.Errorf("assistant slot %d reasoning = %q", i, reasonings[i])
			}
		} else {
			withoutReasoning++
		}
	}
	if withReasoning != 1 || withoutReasoning != 1 {
		t.Errorf("expected 1 assistant w/ reasoning + 1 w/o, got %d / %d", withReasoning, withoutReasoning)
	}
}

func TestBuildMessages_UserInlineData(t *testing.T) {
	msgs, reasonings := buildMessages("", nil, "Describe this", []agentic.InlineData{
		{MIMEType: "image/png", Data: []byte("fake-png-data")},
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(reasonings) != len(msgs) {
		t.Fatalf("reasonings slice must align with messages: got %d vs %d", len(reasonings), len(msgs))
	}
	raw, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	rawStr := string(raw)
	if !strings.Contains(rawStr, `"type":"image_url"`) {
		t.Fatalf("expected image_url content part, got %s", rawStr)
	}
	if !strings.Contains(rawStr, "data:image/png;base64,ZmFrZS1wbmctZGF0YQ==") {
		t.Fatalf("expected data URL image payload, got %s", rawStr)
	}
}

func TestBuildMessages_HistoryUserInlineData(t *testing.T) {
	history := []message.AgentMessage{
		{
			Role:       message.RoleUser,
			Content:    "Earlier image",
			InlineData: []agentic.InlineData{{MIMEType: "image/jpeg", Data: []byte("jpeg")}},
		},
	}
	msgs, _ := buildMessages("", history, "", nil)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	raw, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	rawStr := string(raw)
	if !strings.Contains(rawStr, "data:image/jpeg;base64,anBlZw==") {
		t.Fatalf("expected historical image payload, got %s", rawStr)
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

func TestResponseFormatFromSchema_InvalidSchema(t *testing.T) {
	_, err := responseFormatFromSchema([]byte(`{"type":`))
	if err == nil {
		t.Fatal("expected error for invalid schema")
	}
}

func TestEmitToolUseEvents_PreservesOrderAndFallbackIDs(t *testing.T) {
	events := make(chan providers.StreamEvent, 4)
	toolAccums := map[int64]*toolAccum{
		2: {name: "second", argsJSON: stringsBuilder(`{"b":2}`)},
		1: {name: "first", argsJSON: stringsBuilder(`{"a":1}`)},
	}
	order := []int64{1, 2}

	if err := emitToolUseEvents(toolAccums, order, events); err != nil {
		t.Fatalf("emitToolUseEvents returned error: %v", err)
	}
	close(events)

	var calls []agentic.ToolCall
	for event := range events {
		toolEvent, ok := event.(providers.ToolUseEvent)
		if !ok {
			t.Fatalf("unexpected event type %T", event)
		}
		calls = append(calls, toolEvent.Call)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Name != "first" || calls[0].ID != "tool_call_1" {
		t.Fatalf("unexpected first call: %+v", calls[0])
	}
	if calls[1].Name != "second" || calls[1].ID != "tool_call_2" {
		t.Fatalf("unexpected second call: %+v", calls[1])
	}
}

func TestEmitToolUseEvents_InvalidJSON(t *testing.T) {
	events := make(chan providers.StreamEvent, 1)
	toolAccums := map[int64]*toolAccum{0: {name: "bad", argsJSON: stringsBuilder(`{"a":`)}}
	if err := emitToolUseEvents(toolAccums, []int64{0}, events); err == nil {
		t.Fatal("expected error for invalid json")
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

func stringsBuilder(v string) strings.Builder {
	var b strings.Builder
	b.WriteString(v)
	return b
}

// Verify interface compliance at compile time.
var _ providers.Streamer = (*Client)(nil)
