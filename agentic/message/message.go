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

// ForBudget converts to minimal budget.Message for token counting.
func (m AgentMessage) ForBudget() budget.Message {
	content := m.Content
	for _, tc := range m.ToolCalls {
		content += tc.Name + string(tc.Input)
	}
	for _, tr := range m.ToolResults {
		content += string(tr.Output)
	}
	return budget.Message{Role: m.Role, Content: content}
}

// ForBudgetSlice converts a slice of AgentMessage for token counting.
func ForBudgetSlice(msgs []AgentMessage) []budget.Message {
	out := make([]budget.Message, len(msgs))
	for i, m := range msgs {
		out[i] = m.ForBudget()
	}
	return out
}

// FromBudget creates an AgentMessage from a budget.Message.
// This is useful for loading legacy history or summaries.
func FromBudget(msg budget.Message) AgentMessage {
	return AgentMessage{
		Role:    msg.Role,
		Content: msg.Content,
	}
}

// FromBudgetSlice converts a slice of budget.Message to AgentMessage.
func FromBudgetSlice(msgs []budget.Message) []AgentMessage {
	out := make([]AgentMessage, len(msgs))
	for i, m := range msgs {
		out[i] = FromBudget(m)
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

	budgetMsgs := ForBudgetSlice(messages)
	compacted, summary, changed, err := mgr.CompactIfNeeded(ctx, budgetMsgs)
	if err != nil || !changed {
		return messages, summary, changed, err
	}

	// Compacted result: [summary, kept_msg_1, kept_msg_2, ...]
	// Keep original AgentMessages (with tool data) from the end of the input.
	keptCount := max(0, len(compacted)-1)
	startIdx := max(0, len(messages)-keptCount)

	result := make([]AgentMessage, 0, keptCount+1)
	result = append(result, AgentMessage{
		Role:      RoleSystem,
		Content:   summary,
		Timestamp: time.Now(),
	})
	result = append(result, messages[startIdx:]...)

	return result, summary, true, nil
}
