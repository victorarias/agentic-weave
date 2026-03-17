package providers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

type stubStreamer struct {
	streamFn func(ctx context.Context, input Input) (<-chan StreamEvent, error)
}

func (s stubStreamer) Stream(ctx context.Context, input Input) (<-chan StreamEvent, error) {
	return s.streamFn(ctx, input)
}

func TestCollectDecision_CollectsReplyAndTools(t *testing.T) {
	ch := make(chan StreamEvent, 4)
	ch <- TextDeltaEvent{Delta: "Hello"}
	ch <- ToolUseEvent{Call: agentic.ToolCall{ID: "tool-1", Name: "noop"}}
	ch <- DoneEvent{StopReason: "tool_use", Usage: &usage.Usage{Input: 10, Output: 4}}
	close(ch)

	decision, err := CollectDecision(context.Background(), ch, nil)
	if err != nil {
		t.Fatalf("CollectDecision returned error: %v", err)
	}
	if decision.Reply != "Hello" {
		t.Fatalf("unexpected reply: %q", decision.Reply)
	}
	if len(decision.ToolCalls) != 1 || decision.ToolCalls[0].Name != "noop" {
		t.Fatalf("unexpected tool calls: %#v", decision.ToolCalls)
	}
	if decision.StopReason != usage.StopReasonTool {
		t.Fatalf("unexpected stop reason: %v", decision.StopReason)
	}
}

func TestCollectDecision_ReturnsStreamError(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ch <- ErrorEvent{Err: errors.New("boom")}
	close(ch)

	_, err := CollectDecision(context.Background(), ch, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "llm stream failed") {
		t.Fatalf("expected wrapped boom error, got %v", err)
	}
}

func TestStreamingLoopDecider_RetriesTransientErrorWithoutOutput(t *testing.T) {
	calls := 0
	streamer := stubStreamer{streamFn: func(ctx context.Context, input Input) (<-chan StreamEvent, error) {
		calls++
		ch := make(chan StreamEvent, 2)
		if calls == 1 {
			ch <- ErrorEvent{Err: errors.New(`provider stream: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)}
			close(ch)
			return ch, nil
		}
		ch <- TextDeltaEvent{Delta: "Retry ok"}
		ch <- DoneEvent{StopReason: "end_turn", Usage: nil}
		close(ch)
		return ch, nil
	}}

	decider := NewStreamingLoopDecider(streamer, func(string) {})
	decision, err := decider.Decide(context.Background(), loop.Input{
		SystemPrompt: "You are helpful.",
		History:      []message.AgentMessage{{Role: message.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
	if decision.Reply != "Retry ok" {
		t.Fatalf("unexpected reply: %q", decision.Reply)
	}
}

func TestStreamingLoopDecider_RetriesAnthropicInternalServerErrorWithoutOutput(t *testing.T) {
	calls := 0
	streamer := stubStreamer{streamFn: func(ctx context.Context, input Input) (<-chan StreamEvent, error) {
		calls++
		ch := make(chan StreamEvent, 2)
		if calls == 1 {
			ch <- ErrorEvent{Err: errors.New(`anthropic stream: received error while streaming: {"type":"error","error":{"details":null,"type":"api_error","message":"Internal server error"},"request_id":"req_test"}`)}
			close(ch)
			return ch, nil
		}
		ch <- TextDeltaEvent{Delta: "Retry ok"}
		ch <- DoneEvent{StopReason: "end_turn", Usage: nil}
		close(ch)
		return ch, nil
	}}

	decider := NewStreamingLoopDecider(streamer, func(string) {})
	decision, err := decider.Decide(context.Background(), loop.Input{
		SystemPrompt: "You are helpful.",
		History:      []message.AgentMessage{{Role: message.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
	if decision.Reply != "Retry ok" {
		t.Fatalf("unexpected reply: %q", decision.Reply)
	}
}

func TestStreamingLoopDecider_DoesNotRetryAfterOutputStarted(t *testing.T) {
	calls := 0
	streamer := stubStreamer{streamFn: func(ctx context.Context, input Input) (<-chan StreamEvent, error) {
		calls++
		ch := make(chan StreamEvent, 2)
		ch <- TextDeltaEvent{Delta: "partial"}
		ch <- ErrorEvent{Err: errors.New(`provider stream: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)}
		close(ch)
		return ch, nil
	}}

	decider := NewStreamingLoopDecider(streamer, func(string) {})
	_, err := decider.Decide(context.Background(), loop.Input{
		SystemPrompt: "You are helpful.",
		History:      []message.AgentMessage{{Role: message.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected no retry after partial output, got %d attempts", calls)
	}
}

func TestStreamingLoopDecider_OnDecisionIncludesUsageAndStep(t *testing.T) {
	streamer := stubStreamer{streamFn: func(ctx context.Context, input Input) (<-chan StreamEvent, error) {
		ch := make(chan StreamEvent, 1)
		u := usage.Usage{Input: 10, Output: 4, CacheReadInput: 20, CacheCreationInput: 3}
		ch <- DoneEvent{StopReason: "end_turn", Usage: &u}
		close(ch)
		return ch, nil
	}}

	decider := NewStreamingLoopDecider(streamer, func(string) {})
	var meta DecisionMeta
	decider.OnDecision(func(m DecisionMeta) { meta = m })

	if _, err := decider.Decide(context.Background(), loop.Input{Turn: 2, History: []message.AgentMessage{{Role: message.RoleUser, Content: "hi"}}}); err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	if meta.Step != 3 {
		t.Fatalf("expected step 3 from loop input turn, got %d", meta.Step)
	}
	if meta.Usage == nil || meta.Usage.Input != 10 || meta.Usage.Output != 4 {
		t.Fatalf("unexpected usage: %#v", meta.Usage)
	}
}

func TestCollectDecision_NormalizesAlreadyNormalizedStopReasons(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ch <- DoneEvent{StopReason: "tool"}
	close(ch)

	decision, err := CollectDecision(context.Background(), ch, nil)
	if err != nil {
		t.Fatalf("CollectDecision returned error: %v", err)
	}
	if decision.StopReason != usage.StopReasonTool {
		t.Fatalf("expected tool stop reason, got %v", decision.StopReason)
	}
}

func TestStreamingLoopDecider_RetriesUnexpectedStreamTerminationWithoutOutput(t *testing.T) {
	calls := 0
	streamer := stubStreamer{streamFn: func(ctx context.Context, input Input) (<-chan StreamEvent, error) {
		calls++
		ch := make(chan StreamEvent, 1)
		if calls == 1 {
			close(ch)
			return ch, nil
		}
		ch <- DoneEvent{StopReason: "end_turn"}
		close(ch)
		return ch, nil
	}}

	decider := NewStreamingLoopDecider(streamer, func(string) {})
	if _, err := decider.Decide(context.Background(), loop.Input{History: []message.AgentMessage{{Role: message.RoleUser, Content: "hi"}}}); err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected retry after unexpected stream termination, got %d attempts", calls)
	}
}
