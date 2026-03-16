package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestApplyPromptCaching_Automatic(t *testing.T) {
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "You are helpful."}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}

	applyPromptCaching(&req, CacheModeAutomatic, "")

	// Top-level CacheControl should be set.
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"cache_control"`) {
		t.Errorf("expected top-level cache_control in request, got: %s", raw)
	}

	// System block should NOT have cache_control (automatic handles it).
	if req.System[0].CacheControl.Type != "" {
		t.Errorf("expected no cache_control on system block in automatic mode")
	}
}

func TestApplyPromptCaching_Explicit_SystemOnly(t *testing.T) {
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "You are helpful."}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}

	applyPromptCaching(&req, CacheModeExplicit, "")

	// Top-level CacheControl should NOT be set.
	if req.CacheControl.Type != "" {
		t.Errorf("expected no top-level cache_control in explicit mode")
	}

	// System block should have cache_control.
	if req.System[0].CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control on system block, got type=%q", req.System[0].CacheControl.Type)
	}

	// Last message's last block should have cache_control.
	lastMsg := req.Messages[len(req.Messages)-1]
	lastBlock := lastMsg.Content[len(lastMsg.Content)-1]
	if lastBlock.OfText == nil || lastBlock.OfText.CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control on last message block")
	}
}

func TestApplyPromptCaching_Explicit_WithTools(t *testing.T) {
	toolSchema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "system prompt"}},
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &anthropic.ToolParam{Name: "search", InputSchema: schemaFromRaw(toolSchema), Description: anthropic.String("search")}},
			{OfTool: &anthropic.ToolParam{Name: "read", InputSchema: schemaFromRaw(toolSchema), Description: anthropic.String("read")}},
		},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("search for X"))},
	}

	applyPromptCaching(&req, CacheModeExplicit, "")

	// System block should have cache_control.
	if req.System[0].CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control on system block")
	}

	// First tool should NOT have cache_control.
	if req.Tools[0].OfTool.CacheControl.Type != "" {
		t.Errorf("expected no cache_control on first tool")
	}

	// Last tool should have cache_control.
	if req.Tools[1].OfTool.CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control on last tool, got type=%q", req.Tools[1].OfTool.CacheControl.Type)
	}

	// Last message block should have cache_control.
	lastMsg := req.Messages[len(req.Messages)-1]
	lastBlock := lastMsg.Content[len(lastMsg.Content)-1]
	if lastBlock.OfText == nil || lastBlock.OfText.CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control on last message block")
	}
}

func TestApplyPromptCaching_Explicit_ToolResultBlock(t *testing.T) {
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "system"}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
			anthropic.NewAssistantMessage(
				anthropic.NewToolUseBlock("call-1", map[string]any{"q": "test"}, "search"),
			),
			anthropic.NewUserMessage(
				anthropic.NewToolResultBlock("call-1", "result text", false),
			),
		},
	}

	applyPromptCaching(&req, CacheModeExplicit, "")

	lastMsg := req.Messages[len(req.Messages)-1]
	lastBlock := lastMsg.Content[len(lastMsg.Content)-1]
	if lastBlock.OfToolResult == nil {
		t.Fatal("expected last block to be tool_result")
	}
	if lastBlock.OfToolResult.CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control on tool_result block, got type=%q", lastBlock.OfToolResult.CacheControl.Type)
	}
}

func TestApplyPromptCaching_Explicit_ToolUseBlock(t *testing.T) {
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "system"}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
			anthropic.NewAssistantMessage(
				anthropic.NewToolUseBlock("call-1", map[string]any{"q": "test"}, "search"),
			),
		},
	}

	applyPromptCaching(&req, CacheModeExplicit, "")

	lastMsg := req.Messages[len(req.Messages)-1]
	lastBlock := lastMsg.Content[len(lastMsg.Content)-1]
	if lastBlock.OfToolUse == nil {
		t.Fatal("expected last block to be tool_use")
	}
	if lastBlock.OfToolUse.CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control on tool_use block, got type=%q", lastBlock.OfToolUse.CacheControl.Type)
	}
}

func TestApplyPromptCaching_Explicit_NoSystem(t *testing.T) {
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}

	// Should not panic with no system prompt.
	applyPromptCaching(&req, CacheModeExplicit, "")

	lastMsg := req.Messages[len(req.Messages)-1]
	lastBlock := lastMsg.Content[len(lastMsg.Content)-1]
	if lastBlock.OfText == nil || lastBlock.OfText.CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control on last message block even without system")
	}
}

func TestApplyPromptCaching_Explicit_EmptyMessages(t *testing.T) {
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
	}

	// Should not panic with empty messages.
	applyPromptCaching(&req, CacheModeExplicit, "")
}

func TestApplyPromptCaching_NilRequest(t *testing.T) {
	// Should not panic.
	applyPromptCaching(nil, CacheModeAutomatic, "")
	applyPromptCaching(nil, CacheModeExplicit, "")
}

func TestCacheModeDefault_IsAutomatic(t *testing.T) {
	var mode CacheMode
	if mode != CacheModeAutomatic {
		t.Errorf("expected zero value of CacheMode to be CacheModeAutomatic, got %d", mode)
	}
}

func TestApplyPromptCaching_Automatic_1hTTL(t *testing.T) {
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "You are helpful."}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}

	applyPromptCaching(&req, CacheModeAutomatic, anthropic.CacheControlEphemeralTTLTTL1h)

	if req.CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control type=ephemeral, got %q", req.CacheControl.Type)
	}
	if req.CacheControl.TTL != anthropic.CacheControlEphemeralTTLTTL1h {
		t.Errorf("expected cache_control ttl=1h, got %q", req.CacheControl.TTL)
	}

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"ttl":"1h"`) {
		t.Errorf("expected ttl:1h in serialized request, got: %s", raw)
	}
}

func TestApplyPromptCaching_Explicit_1hTTL(t *testing.T) {
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "system prompt"}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}

	applyPromptCaching(&req, CacheModeExplicit, anthropic.CacheControlEphemeralTTLTTL1h)

	// System block should have TTL.
	if req.System[0].CacheControl.TTL != anthropic.CacheControlEphemeralTTLTTL1h {
		t.Errorf("expected ttl=1h on system block, got %q", req.System[0].CacheControl.TTL)
	}

	// Last message block should have TTL.
	lastMsg := req.Messages[len(req.Messages)-1]
	lastBlock := lastMsg.Content[len(lastMsg.Content)-1]
	if lastBlock.OfText == nil {
		t.Fatal("expected text block")
	}
	if lastBlock.OfText.CacheControl.TTL != anthropic.CacheControlEphemeralTTLTTL1h {
		t.Errorf("expected ttl=1h on message block, got %q", lastBlock.OfText.CacheControl.TTL)
	}
}

func TestApplyPromptCaching_Disabled_OmitsCacheControl(t *testing.T) {
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "system prompt"}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}

	applyPromptCaching(&req, CacheModeDisabled, "")

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"cache_control"`) {
		t.Errorf("expected no cache_control when caching disabled, got: %s", raw)
	}
}

func TestApplyPromptCaching_DefaultTTL_OmitsField(t *testing.T) {
	req := anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: "system prompt"}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}

	// Empty TTL = default (5m), should NOT include "ttl" in JSON.
	applyPromptCaching(&req, CacheModeAutomatic, "")

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"ttl"`) {
		t.Errorf("expected no ttl field when using default, got: %s", raw)
	}
}

func TestParseCacheTTL(t *testing.T) {
	tests := []struct {
		input string
		want  anthropic.CacheControlEphemeralTTL
	}{
		{"", ""},
		{"5m", anthropic.CacheControlEphemeralTTLTTL5m},
		{"1h", anthropic.CacheControlEphemeralTTLTTL1h},
		{"1H", anthropic.CacheControlEphemeralTTLTTL1h},
		{" 1h ", anthropic.CacheControlEphemeralTTLTTL1h},
		{"off", ""},
		{"OFF", ""},
		{"invalid", ""},
	}

	for _, tt := range tests {
		got := parseCacheTTL(tt.input)
		if got != tt.want {
			t.Errorf("parseCacheTTL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
