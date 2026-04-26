package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

// DecisionMeta captures one completed provider decision step.
type DecisionMeta struct {
	Reply     string
	Reasoning string
	ToolCalls []agentic.ToolCall
	Usage     *usage.Usage
	Step      int
}

// StreamingLoopDecider adapts a provider Streamer to loop.Decider.
//
// The loop already appends the current user message into History before each
// Decide() call, so this adapter intentionally leaves Input.UserMessage empty to
// avoid duplicating the latest user turn in providers that accept both fields.
type StreamingLoopDecider struct {
	streamer         Streamer
	onDelta          func(string)
	onReasoningDelta func(string)
	onDecision       func(DecisionMeta)
}

// NewStreamingLoopDecider creates a loop decider backed by a provider Streamer.
func NewStreamingLoopDecider(streamer Streamer, onDelta func(string)) *StreamingLoopDecider {
	if onDelta == nil {
		onDelta = func(string) {}
	}
	return &StreamingLoopDecider{
		streamer: streamer,
		onDelta:  onDelta,
	}
}

// OnDecision registers a callback invoked after each successful decision step.
func (d *StreamingLoopDecider) OnDecision(fn func(DecisionMeta)) {
	d.onDecision = fn
}

// OnReasoningDelta registers a callback invoked for each reasoning fragment as
// it streams in. Callers use this to surface a live reasoning trace in their
// UI alongside the existing onDelta text stream. The accumulated reasoning is
// also delivered in DecisionMeta.Reasoning at step boundaries.
func (d *StreamingLoopDecider) OnReasoningDelta(fn func(string)) {
	d.onReasoningDelta = fn
}

// Decide implements loop.Decider.
func (d *StreamingLoopDecider) Decide(ctx context.Context, in loop.Input) (loop.Decision, error) {
	if d == nil || d.streamer == nil {
		return loop.Decision{}, errors.New("providers: loop decider streamer is required")
	}

	const (
		maxAttempts = 3
		baseBackoff = 250 * time.Millisecond
	)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		decision, err, emittedOutput := d.decideOnce(ctx, in)
		if err != nil {
			if shouldRetryStreamFailure(ctx, err, attempt, maxAttempts, emittedOutput) {
				if waitErr := waitForRetryBackoff(ctx, attempt, baseBackoff); waitErr != nil {
					return loop.Decision{}, waitErr
				}
				continue
			}
			return loop.Decision{}, err
		}

		if d.onDecision != nil {
			d.onDecision(DecisionMeta{
				Reply:     decision.Reply,
				Reasoning: decision.Reasoning,
				ToolCalls: decision.ToolCalls,
				Usage:     decision.Usage,
				Step:      in.Turn + 1,
			})
		}
		return loop.Decision{
			Reply:      decision.Reply,
			ToolCalls:  decision.ToolCalls,
			Usage:      decision.Usage,
			StopReason: decision.StopReason,
		}, nil
	}

	return loop.Decision{}, errors.New("providers: llm stream failed after retries")
}

func (d *StreamingLoopDecider) decideOnce(ctx context.Context, in loop.Input) (Decision, error, bool) {
	stream, err := d.streamer.Stream(ctx, Input{
		SystemPrompt: in.SystemPrompt,
		UserMessage:  "",
		History:      in.History,
		Tools:        in.Tools,
	})
	if err != nil {
		return Decision{}, err, false
	}

	var emittedOutput bool
	opts := []CollectOption{
		WithOnDelta(func(delta string) {
			d.onDelta(delta)
			emittedOutput = true
		}),
	}
	if d.onReasoningDelta != nil {
		opts = append(opts, WithOnReasoningDelta(d.onReasoningDelta))
	}
	decision, err := CollectDecision(ctx, stream, opts...)
	if len(decision.ToolCalls) > 0 {
		emittedOutput = true
	}
	if strings.TrimSpace(decision.Reply) != "" {
		emittedOutput = true
	}
	return decision, err, emittedOutput
}

func shouldRetryStreamFailure(ctx context.Context, err error, attempt int, maxAttempts int, emittedOutput bool) bool {
	if err == nil || attempt >= maxAttempts || emittedOutput {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return isTransientLLMError(err)
}

func waitForRetryBackoff(ctx context.Context, attempt int, base time.Duration) error {
	if attempt <= 0 {
		attempt = 1
	}
	wait := time.Duration(attempt) * base
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isTransientLLMError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	transientNeedles := []string{
		"api_error",
		"overloaded_error",
		"rate_limit_error",
		"rate limit",
		"temporarily unavailable",
		"server overloaded",
		"upstream timeout",
		"gateway timeout",
		"connection reset",
		"unexpected eof",
		"eof",
		"stream ended without done event",
		"http 429",
		"http 502",
		"http 503",
		"http 504",
	}
	for _, needle := range transientNeedles {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

var _ loop.Decider = (*StreamingLoopDecider)(nil)

func (d DecisionMeta) String() string {
	return fmt.Sprintf("step=%d tools=%d", d.Step, len(d.ToolCalls))
}
