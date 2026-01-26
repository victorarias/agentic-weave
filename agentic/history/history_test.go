package history

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
)

func TestMemoryStoreWithToolCalls(t *testing.T) {
	store := NewMemoryStore()

	// Store assistant message with tool calls
	assistantMsg := message.AgentMessage{
		Role:    message.RoleAssistant,
		Content: "I'll search for that.",
		ToolCalls: []agentic.ToolCall{
			{ID: "tc1", Name: "search", Input: json.RawMessage(`{"q":"test"}`)},
		},
	}
	if err := store.Append(context.Background(), assistantMsg); err != nil {
		t.Fatalf("append assistant failed: %v", err)
	}

	// Store tool result message
	toolMsg := message.AgentMessage{
		Role: message.RoleTool,
		ToolResults: []agentic.ToolResult{
			{ID: "tc1", Name: "search", Output: json.RawMessage(`"found 10 results"`)},
		},
	}
	if err := store.Append(context.Background(), toolMsg); err != nil {
		t.Fatalf("append tool result failed: %v", err)
	}

	msgs, _ := store.Load(context.Background())
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if len(msgs[0].ToolCalls) != 1 || msgs[0].ToolCalls[0].Name != "search" {
		t.Fatalf("tool call not preserved: %#v", msgs[0])
	}
	if len(msgs[1].ToolResults) != 1 || msgs[1].ToolResults[0].Name != "search" {
		t.Fatalf("tool result not preserved: %#v", msgs[1])
	}
}
