package providers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic/context/budget"
)

type budgetableMessage struct {
	role    string
	content string
}

func (m budgetableMessage) BudgetRole() string    { return m.role }
func (m budgetableMessage) BudgetContent() string { return m.content }

func TestStreamingCompactor_EmptyMessagesSkipsStream(t *testing.T) {
	calls := 0
	streamer := stubStreamer{streamFn: func(ctx context.Context, input Input) (<-chan StreamEvent, error) {
		calls++
		ch := make(chan StreamEvent)
		close(ch)
		return ch, nil
	}}
	compactor := NewStreamingCompactor(streamer, nil)

	summary, err := compactor.Compact(context.Background(), nil)
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if summary != "" {
		t.Fatalf("expected empty summary, got %q", summary)
	}
	if calls != 0 {
		t.Fatalf("expected no stream calls, got %d", calls)
	}
}

func TestStreamingCompactor_UsesDefaultPromptAndCollectsSummary(t *testing.T) {
	streamer := stubStreamer{streamFn: func(ctx context.Context, input Input) (<-chan StreamEvent, error) {
		if input.SystemPrompt != DefaultCompactionSystemPrompt {
			t.Fatalf("unexpected system prompt: %q", input.SystemPrompt)
		}
		if !strings.Contains(input.UserMessage, "user: hello") {
			t.Fatalf("expected user message content in compaction source, got %q", input.UserMessage)
		}
		if !strings.Contains(input.UserMessage, "unknown: missing role") {
			t.Fatalf("expected unknown role fallback, got %q", input.UserMessage)
		}
		ch := make(chan StreamEvent, 2)
		ch <- TextDeltaEvent{Delta: "summary"}
		ch <- DoneEvent{StopReason: "end_turn"}
		close(ch)
		return ch, nil
	}}
	compactor := NewStreamingCompactor(streamer, nil)

	summary, err := compactor.Compact(context.Background(), []budget.Budgetable{
		budgetableMessage{role: "user", content: "hello"},
		budgetableMessage{role: "", content: "missing role"},
	})
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if summary != "summary" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestStreamingCompactor_UsesCustomPromptProfile(t *testing.T) {
	streamer := stubStreamer{streamFn: func(ctx context.Context, input Input) (<-chan StreamEvent, error) {
		if input.SystemPrompt != "custom-prompt" {
			t.Fatalf("unexpected system prompt: %q", input.SystemPrompt)
		}
		ch := make(chan StreamEvent, 2)
		ch <- TextDeltaEvent{Delta: "ok"}
		ch <- DoneEvent{StopReason: "end_turn"}
		close(ch)
		return ch, nil
	}}
	compactor := NewStreamingCompactor(streamer, StaticCompactionPromptProfile{SystemPrompt: "custom-prompt"})

	summary, err := compactor.Compact(context.Background(), []budget.Budgetable{budgetableMessage{role: "user", content: "hello"}})
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if summary != "ok" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestStreamingCompactor_StreamError(t *testing.T) {
	streamer := stubStreamer{streamFn: func(ctx context.Context, input Input) (<-chan StreamEvent, error) {
		ch := make(chan StreamEvent, 1)
		ch <- ErrorEvent{Err: errors.New("boom")}
		close(ch)
		return ch, nil
	}}
	compactor := NewStreamingCompactor(streamer, nil)

	if _, err := compactor.Compact(context.Background(), []budget.Budgetable{budgetableMessage{role: "user", content: "hello"}}); err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "llm stream failed") {
		t.Fatalf("expected wrapped boom error, got %v", err)
	}
}
