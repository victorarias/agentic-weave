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
	InlineData  []agentic.InlineData // User-provided images; not counted in BudgetContent.
	Timestamp   time.Time

	// ReasoningContent is the textual reasoning trace produced by a prior
	// assistant turn on a reasoning model (DeepSeek-style "reasoning_content",
	// OpenRouter's normalized "reasoning"). Set only on assistant messages.
	//
	// Providers that need the field on the wire (e.g. DeepSeek V4 Pro will 400
	// on multi-turn replay if a prior assistant turn that produced reasoning
	// is sent back without it) MUST round-trip this through to the upstream
	// request. Providers that do not are free to ignore it.
	ReasoningContent string
}

// BudgetRole implements budget.Budgetable.
func (m AgentMessage) BudgetRole() string {
	return m.Role
}

// BudgetContent implements budget.Budgetable.
// Returns all content concatenated for token estimation.
//
// ReasoningContent is included because providers that round-trip it (DeepSeek,
// OpenRouter reasoning models) send it back on the wire as part of the prior
// assistant turn — leaving it out would undercount prompt size and skip
// compaction when the hidden trace is what's pushing past the budget.
func (m AgentMessage) BudgetContent() string {
	content := m.Content
	if m.ReasoningContent != "" {
		content += m.ReasoningContent
	}
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
