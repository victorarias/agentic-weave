package providers

import (
	"context"
	"errors"
	"strings"

	"github.com/victorarias/agentic-weave/agentic/context/budget"
)

const DefaultCompactionSystemPrompt = "Summarize the conversation so far. Preserve decisions, tasks, and key context. Keep it concise."

// CompactionPromptProfile provides a purpose-specific system prompt for history compaction.
type CompactionPromptProfile interface {
	CompactionSystemPrompt() string
}

// StaticCompactionPromptProfile is a fixed prompt profile.
type StaticCompactionPromptProfile struct {
	SystemPrompt string
}

func (p StaticCompactionPromptProfile) CompactionSystemPrompt() string {
	return p.SystemPrompt
}

// StreamingCompactor adapts a provider Streamer to budget.Compactor.
type StreamingCompactor struct {
	streamer      Streamer
	promptProfile CompactionPromptProfile
}

// NewStreamingCompactor creates a provider-neutral budget compactor.
func NewStreamingCompactor(streamer Streamer, promptProfile CompactionPromptProfile) *StreamingCompactor {
	return &StreamingCompactor{
		streamer:      streamer,
		promptProfile: promptProfile,
	}
}

// Compact summarizes the provided messages with the configured provider.
func (c *StreamingCompactor) Compact(ctx context.Context, messages []budget.Budgetable) (string, error) {
	if c == nil || c.streamer == nil {
		return "", errors.New("providers: compactor streamer is required")
	}
	if len(messages) == 0 {
		return "", nil
	}

	systemPrompt := DefaultCompactionSystemPrompt
	if c.promptProfile != nil {
		if override := strings.TrimSpace(c.promptProfile.CompactionSystemPrompt()); override != "" {
			systemPrompt = override
		}
	}

	var source strings.Builder
	for _, msg := range messages {
		role := strings.TrimSpace(msg.BudgetRole())
		if role == "" {
			role = "unknown"
		}
		source.WriteString(role)
		source.WriteString(": ")
		source.WriteString(msg.BudgetContent())
		source.WriteString("\n")
	}

	decision, err := Decide(ctx, c.streamer, Input{
		SystemPrompt: systemPrompt,
		UserMessage:  source.String(),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(decision.Reply), nil
}

var _ budget.Compactor = (*StreamingCompactor)(nil)
