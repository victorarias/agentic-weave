package anthropic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic/context/budget"
)

type budgetMsg struct {
	role    string
	content string
}

func (m budgetMsg) BudgetRole() string    { return m.role }
func (m budgetMsg) BudgetContent() string { return m.content }

type captureStreamer struct {
	calls  int
	input  Input
	events []StreamEvent
	err    error
}

func (s *captureStreamer) Stream(ctx context.Context, input Input) (<-chan StreamEvent, error) {
	s.calls++
	s.input = input
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan StreamEvent, len(s.events))
	for _, ev := range s.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

type staticProfile string

func (p staticProfile) CompactionSystemPrompt() string { return string(p) }

func TestStreamingCompactor_EmptyInput(t *testing.T) {
	streamer := &captureStreamer{}
	compactor := NewStreamingCompactor(streamer, nil)

	got, err := compactor.Compact(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty summary, got %q", got)
	}
	if streamer.calls != 0 {
		t.Fatalf("expected no stream call, got %d", streamer.calls)
	}
}

func TestStreamingCompactor_DefaultPrompt(t *testing.T) {
	streamer := &captureStreamer{
		events: []StreamEvent{
			TextDeltaEvent{Delta: "summary"},
			DoneEvent{StopReason: "end_turn"},
		},
	}
	compactor := NewStreamingCompactor(streamer, nil)

	got, err := compactor.Compact(context.Background(), []budget.Budgetable{
		budgetMsg{role: "user", content: "hello"},
		budgetMsg{role: "", content: "missing role"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "summary" {
		t.Fatalf("expected summary, got %q", got)
	}
	if streamer.input.SystemPrompt != DefaultCompactionSystemPrompt {
		t.Fatalf("unexpected system prompt: %q", streamer.input.SystemPrompt)
	}
	if len(streamer.input.Tools) != 0 || len(streamer.input.History) != 0 {
		t.Fatalf("expected no tools/history in compaction input")
	}
	if !strings.Contains(streamer.input.UserMessage, "user: hello") {
		t.Fatalf("expected user message content in compaction source, got %q", streamer.input.UserMessage)
	}
	if !strings.Contains(streamer.input.UserMessage, "unknown: missing role") {
		t.Fatalf("expected unknown role fallback, got %q", streamer.input.UserMessage)
	}
}

func TestStreamingCompactor_CustomPromptProfile(t *testing.T) {
	streamer := &captureStreamer{
		events: []StreamEvent{
			TextDeltaEvent{Delta: "ok"},
			DoneEvent{StopReason: "end_turn"},
		},
	}
	compactor := NewStreamingCompactor(streamer, staticProfile("custom-prompt"))

	_, err := compactor.Compact(context.Background(), []budget.Budgetable{
		budgetMsg{role: "user", content: "hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if streamer.input.SystemPrompt != "custom-prompt" {
		t.Fatalf("expected custom prompt, got %q", streamer.input.SystemPrompt)
	}
}

func TestStreamingCompactor_StreamError(t *testing.T) {
	streamer := &captureStreamer{
		events: []StreamEvent{
			ErrorEvent{Err: errors.New("boom")},
		},
	}
	compactor := NewStreamingCompactor(streamer, nil)

	_, err := compactor.Compact(context.Background(), []budget.Budgetable{
		budgetMsg{role: "user", content: "hello"},
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom, got %v", err)
	}
}

func TestStreamingCompactor_RequiresDoneEvent(t *testing.T) {
	streamer := &captureStreamer{
		events: []StreamEvent{
			TextDeltaEvent{Delta: "partial"},
		},
	}
	compactor := NewStreamingCompactor(streamer, nil)

	_, err := compactor.Compact(context.Background(), []budget.Budgetable{
		budgetMsg{role: "user", content: "hello"},
	})
	if err == nil || !strings.Contains(err.Error(), "without done event") {
		t.Fatalf("expected missing done event error, got %v", err)
	}
}
