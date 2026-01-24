package message

import (
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
)

func TestToolResultsIncludedInBudget(t *testing.T) {
	msg := AgentMessage{
		Role: RoleTool,
		ToolResults: []agentic.ToolResult{
			{Name: "read", Output: json.RawMessage(`"file contents"`)},
		},
	}

	bm := msg.ForBudget()

	if bm.Content != `"file contents"` {
		t.Errorf("expected tool output in content, got %q", bm.Content)
	}
}
