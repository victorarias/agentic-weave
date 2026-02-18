package anthropic

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

type streamingClient interface {
	Stream(ctx context.Context, input Input) (<-chan StreamEvent, error)
}

// StreamingCompactor adapts the Anthropic streaming client to budget.Compactor.
type StreamingCompactor struct {
	client        streamingClient
	promptProfile CompactionPromptProfile
}

var _ budget.Compactor = (*StreamingCompactor)(nil)

func NewStreamingCompactor(client streamingClient, promptProfile CompactionPromptProfile) *StreamingCompactor {
	return &StreamingCompactor{
		client:        client,
		promptProfile: promptProfile,
	}
}

func (c *StreamingCompactor) Compact(ctx context.Context, messages []budget.Budgetable) (string, error) {
	if c == nil || c.client == nil {
		return "", errors.New("anthropic compactor: client is required")
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

	stream, err := c.client.Stream(ctx, Input{
		SystemPrompt: systemPrompt,
		UserMessage:  source.String(),
	})
	if err != nil {
		return "", err
	}

	var (
		summary strings.Builder
		sawDone bool
	)
	for ev := range stream {
		switch e := ev.(type) {
		case TextDeltaEvent:
			summary.WriteString(e.Delta)
		case DoneEvent:
			sawDone = true
		case ErrorEvent:
			if e.Err != nil {
				return "", e.Err
			}
			return "", errors.New("anthropic compactor stream failed")
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}

	if !sawDone {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", errors.New("anthropic compactor stream ended without done event")
	}
	return strings.TrimSpace(summary.String()), nil
}
