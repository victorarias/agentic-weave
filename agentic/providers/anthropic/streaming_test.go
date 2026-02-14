package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
)

func TestAppendHistory_ToolRoleBecomesUserToolResultMessage(t *testing.T) {
	history := []message.AgentMessage{
		{Role: message.RoleUser, Content: "hi"},
		{Role: message.RoleAssistant, ToolCalls: []agentic.ToolCall{{ID: "toolu_123", Name: "add", Input: json.RawMessage(`{"a":1,"b":2}`)}}},
		{Role: message.RoleTool, ToolResults: []agentic.ToolResult{{ID: "toolu_123", Name: "add", Output: json.RawMessage(`{"sum":3}`)}}},
	}

	msgs := appendHistory(nil, history)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 anthropic messages, got %d", len(msgs))
	}

	// The critical invariant: assistant tool_use message is immediately followed by a user tool_result message.
	if msgs[1].Role != anthropic.MessageParamRoleAssistant {
		t.Fatalf("expected msgs[1] role assistant, got %q", msgs[1].Role)
	}
	if msgs[2].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("expected msgs[2] role user (tool_result container), got %q", msgs[2].Role)
	}
}

func TestAppendHistory_CoalescesConsecutiveToolMessages(t *testing.T) {
	history := []message.AgentMessage{
		{Role: message.RoleUser, Content: "hi"},
		{
			Role: message.RoleAssistant,
			ToolCalls: []agentic.ToolCall{
				{ID: "toolu_1", Name: "a", Input: json.RawMessage(`{}`)},
				{ID: "toolu_2", Name: "b", Input: json.RawMessage(`{}`)},
			},
		},
		{Role: message.RoleTool, ToolResults: []agentic.ToolResult{{ID: "toolu_1", Name: "a", Output: json.RawMessage(`"one"`)}}},
		{Role: message.RoleTool, ToolResults: []agentic.ToolResult{{ID: "toolu_2", Name: "b", Output: json.RawMessage(`"two"`)}}},
	}

	msgs := appendHistory(nil, history)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 anthropic messages, got %d", len(msgs))
	}
	if msgs[1].Role != anthropic.MessageParamRoleAssistant {
		t.Fatalf("expected msgs[1] role assistant, got %q", msgs[1].Role)
	}
	if msgs[2].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("expected msgs[2] role user (tool_result container), got %q", msgs[2].Role)
	}

	raw, err := json.Marshal(msgs[2])
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("expected valid json")
	}
	// Both tool ids should appear in the single next message.
	if !bytes.Contains(raw, []byte("toolu_1")) || !bytes.Contains(raw, []byte("toolu_2")) {
		t.Fatalf("expected coalesced tool results to include both tool ids, got %s", string(raw))
	}
}

func TestCollectDecision_ErrorsOnNilChannel(t *testing.T) {
	_, err := CollectDecision(nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestCollectDecision_PropagatesErrorEvent(t *testing.T) {
	ch := make(chan StreamEvent, 2)
	ch <- ErrorEvent{Err: errors.New("boom")}
	close(ch)

	_, err := CollectDecision(ch)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom, got %v", err)
	}
}

func TestCollectDecision_BuildsDecision(t *testing.T) {
	ch := make(chan StreamEvent, 4)
	ch <- TextDeltaEvent{Delta: "Hello"}
	ch <- ToolUseEvent{Call: agentic.ToolCall{ID: "toolu_1", Name: "noop", Input: json.RawMessage(`{}`)}}
	ch <- DoneEvent{StopReason: "tool_use", Usage: nil}
	close(ch)

	got, err := CollectDecision(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Reply != "Hello" {
		t.Fatalf("expected reply Hello, got %q", got.Reply)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "toolu_1" {
		t.Fatalf("unexpected tool calls: %#v", got.ToolCalls)
	}
	if got.StopReason != "tool_use" {
		t.Fatalf("expected stop tool_use, got %q", got.StopReason)
	}
}

// Compile-time check: Stream exists with the expected method expression type.
var _ func(*Client, context.Context, Input) (<-chan StreamEvent, error) = (*Client).Stream
