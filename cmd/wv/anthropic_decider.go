package main

import (
	"context"
	"strings"

	"github.com/victorarias/agentic-weave/agentic/loop"
	provider "github.com/victorarias/agentic-weave/agentic/providers/anthropic"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

type anthropicDecider struct {
	client      *provider.Client
	maxTokens   int
	temperature *float64
}

func (d anthropicDecider) Decide(ctx context.Context, in loop.Input) (loop.Decision, error) {
	decision, err := d.client.Decide(ctx, provider.Input{
		SystemPrompt: in.SystemPrompt,
		UserMessage:  in.UserMessage,
		History:      in.History,
		Tools:        in.Tools,
		MaxTokens:    d.maxTokens,
		Temperature:  d.temperature,
	})
	if err != nil {
		return loop.Decision{}, err
	}
	return loop.Decision{
		Reply:      decision.Reply,
		ToolCalls:  decision.ToolCalls,
		Usage:      decision.Usage,
		StopReason: normalizeStopReason(decision.StopReason),
	}, nil
}

func normalizeStopReason(stop string) usage.StopReason {
	switch strings.TrimSpace(strings.ToLower(stop)) {
	case "tool_use":
		return usage.StopReasonTool
	case "max_tokens":
		return usage.StopReasonMaxTokens
	case "end_turn", "stop":
		return usage.StopReasonStop
	default:
		normalized := strings.TrimSpace(strings.ToLower(stop))
		if normalized == "" {
			return usage.StopReasonStop
		}
		return usage.StopReason(normalized)
	}
}
