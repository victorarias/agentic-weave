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

	content := msg.BudgetContent()

	if content != `"file contents"` {
		t.Errorf("expected tool output in content, got %q", content)
	}
}

func TestToolErrorsIncludedInBudget(t *testing.T) {
	msg := AgentMessage{
		Role: RoleTool,
		ToolResults: []agentic.ToolResult{
			{Name: "read", Error: &agentic.ToolError{Message: "file not found"}},
		},
	}

	content := msg.BudgetContent()

	if content != "file not found" {
		t.Errorf("expected tool error in content, got %q", content)
	}
}
