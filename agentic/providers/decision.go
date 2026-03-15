package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

// Decision is the provider-neutral result of collecting a streamed response.
//
// Providers emit TextDeltaEvent, ToolUseEvent, and a terminal DoneEvent. This
// shape lets callers consume a full reply without depending on provider-specific
// SDK types or event enums.
type Decision struct {
	Reply      string
	ToolCalls  []agentic.ToolCall
	Usage      *usage.Usage
	StopReason usage.StopReason
}

// Decide streams one provider request and collects it into a Decision.
func Decide(ctx context.Context, streamer Streamer, input Input) (Decision, error) {
	if streamer == nil {
		return Decision{}, errors.New("providers: streamer is required")
	}
	stream, err := streamer.Stream(ctx, input)
	if err != nil {
		return Decision{}, err
	}
	return CollectDecision(ctx, stream, nil)
}

// CollectDecision converts provider stream events into a single Decision.
//
// If onDelta is non-nil, each TextDeltaEvent is forwarded immediately.
func CollectDecision(ctx context.Context, events <-chan StreamEvent, onDelta func(string)) (Decision, error) {
	if events == nil {
		return Decision{}, errors.New("providers: nil events channel")
	}
	if onDelta == nil {
		onDelta = func(string) {}
	}

	var (
		reply         strings.Builder
		calls         []agentic.ToolCall
		usageVal      *usage.Usage
		stop          usage.StopReason
		sawDone       bool
		emittedOutput bool
	)

	for ev := range events {
		switch e := ev.(type) {
		case TextDeltaEvent:
			reply.WriteString(e.Delta)
			onDelta(e.Delta)
			emittedOutput = true
		case ToolUseEvent:
			calls = append(calls, e.Call)
			emittedOutput = true
		case DoneEvent:
			usageVal = e.Usage
			stop = normalizeStopReason(e.StopReason)
			sawDone = true
		case ErrorEvent:
			if e.Err == nil {
				return Decision{}, errors.New("llm stream failed")
			}
			return Decision{}, fmt.Errorf("llm stream failed: %w", e.Err)
		}
		if ctx != nil && ctx.Err() != nil {
			return Decision{}, ctx.Err()
		}
	}

	if !sawDone {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return Decision{}, err
			}
		}
		if emittedOutput {
			return Decision{}, errors.New("providers: stream ended without done event after emitting output")
		}
		return Decision{}, errors.New("providers: stream ended without done event")
	}

	return Decision{
		Reply:      strings.TrimSpace(reply.String()),
		ToolCalls:  calls,
		Usage:      usageVal,
		StopReason: stop,
	}, nil
}

func normalizeStopReason(reason string) usage.StopReason {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "max_tokens", "length":
		return usage.StopReasonMaxTokens
	case "tool_use", "tool_calls", "tool":
		return usage.StopReasonTool
	case "error", "abort":
		return usage.StopReasonError
	case "", "end_turn", "stop":
		return usage.StopReasonStop
	default:
		return usage.StopReasonStop
	}
}
