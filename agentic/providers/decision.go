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
// Providers emit TextDeltaEvent, ToolUseEvent, ReasoningDeltaEvent, and a
// terminal DoneEvent. This shape lets callers consume a full reply without
// depending on provider-specific SDK types or event enums.
//
// Reasoning is the concatenated text from every ReasoningDeltaEvent on the
// stream, in arrival order. It is empty for non-reasoning models. Callers
// that need to round-trip the trace on the next turn (e.g. DeepSeek V4 Pro,
// which 400s without it) can pass this back via
// message.AgentMessage.ReasoningContent.
type Decision struct {
	Reply      string
	Reasoning  string
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
	return CollectDecision(ctx, stream)
}

// CollectOption configures CollectDecision's per-event live callbacks.
type CollectOption func(*collectOptions)

type collectOptions struct {
	onDelta          func(string)
	onReasoningDelta func(string)
}

// WithOnDelta forwards every TextDeltaEvent's text to fn as it arrives.
func WithOnDelta(fn func(string)) CollectOption {
	return func(o *collectOptions) { o.onDelta = fn }
}

// WithOnReasoningDelta forwards every ReasoningDeltaEvent's text to fn as it
// arrives. Use this for live-streaming a reasoning trace to the UI; the
// accumulated text is also available on the returned Decision.Reasoning.
func WithOnReasoningDelta(fn func(string)) CollectOption {
	return func(o *collectOptions) { o.onReasoningDelta = fn }
}

// CollectDecision converts provider stream events into a single Decision.
//
// Use WithOnDelta / WithOnReasoningDelta to subscribe to per-event callbacks
// while the stream drains; the same content is returned aggregated on the
// Decision.
func CollectDecision(ctx context.Context, events <-chan StreamEvent, opts ...CollectOption) (Decision, error) {
	if events == nil {
		return Decision{}, errors.New("providers: nil events channel")
	}
	cfg := collectOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	onDelta := cfg.onDelta
	if onDelta == nil {
		onDelta = func(string) {}
	}
	onReasoningDelta := cfg.onReasoningDelta
	if onReasoningDelta == nil {
		onReasoningDelta = func(string) {}
	}

	var (
		reply         strings.Builder
		reasoning     strings.Builder
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
		case ReasoningDeltaEvent:
			reasoning.WriteString(e.Delta)
			onReasoningDelta(e.Delta)
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
		Reasoning:  reasoning.String(),
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
