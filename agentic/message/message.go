// Package message provides the rich internal message representation for agentic loops.
package message

import (
	"context"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
)

// Role constants for message types.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleSystem    = "system"
)

// AgentMessage is the rich internal message representation.
// Tool calls and results are structured, not flattened to text.
type AgentMessage struct {
	Role        string
	Content     string
	ToolCalls   []agentic.ToolCall
	ToolResults []agentic.ToolResult
	Timestamp   time.Time
}

// BudgetRole implements budget.Budgetable.
func (m AgentMessage) BudgetRole() string {
	return m.Role
}

// BudgetContent implements budget.Budgetable.
// Returns all content concatenated for token estimation.
func (m AgentMessage) BudgetContent() string {
	content := m.Content
	for _, tc := range m.ToolCalls {
		content += tc.Name + string(tc.Input)
	}
	for _, tr := range m.ToolResults {
		content += string(tr.Output)
		if tr.Error != nil {
			content += tr.Error.Message
		}
	}
	return content
}

// ToBudgetable converts a slice of AgentMessage to []budget.Budgetable.
func ToBudgetable(messages []AgentMessage) []budget.Budgetable {
	out := make([]budget.Budgetable, len(messages))
	for i, m := range messages {
		out[i] = m
	}
	return out
}

// CompactIfNeeded wraps budget.Manager.CompactIfNeeded for AgentMessage slices.
// It preserves the full AgentMessage structure for messages after the cut point.
// Returns: (compacted messages, summary text, whether compaction occurred, error).
func CompactIfNeeded(ctx context.Context, mgr budget.Manager, messages []AgentMessage) ([]AgentMessage, string, bool, error) {
	if len(messages) == 0 {
		return messages, "", false, nil
	}

	budgetable := ToBudgetable(messages)
	summary, keepCount, changed, err := mgr.CompactIfNeeded(ctx, budgetable)
	if err != nil || !changed {
		return messages, summary, changed, err
	}

	// Keep original AgentMessages (with tool data) from the end
	startIdx := max(0, len(messages)-keepCount)

	result := make([]AgentMessage, 0, keepCount+1)
	result = append(result, AgentMessage{
		Role:      RoleSystem,
		Content:   summary,
		Timestamp: time.Now(),
	})
	result = append(result, messages[startIdx:]...)

	return result, summary, true, nil
}
