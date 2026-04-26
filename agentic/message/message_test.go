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

func TestReasoningContentCountedInBudget(t *testing.T) {
	// Reasoning traces are round-tripped on the wire for DeepSeek-style
	// providers, so they must contribute to the budget — otherwise a long
	// hidden trace can skip compaction and blow past the provider's context
	// limit on the next turn.
	msg := AgentMessage{
		Role:             RoleAssistant,
		Content:          "answer",
		ReasoningContent: "long internal reasoning trace",
	}

	content := msg.BudgetContent()

	if content != "answerlong internal reasoning trace" {
		t.Errorf("expected reasoning trace folded into budget content, got %q", content)
	}
}
