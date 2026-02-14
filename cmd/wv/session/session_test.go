package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/loop"
)

type staticDecider struct {
	reply string
}

func (d staticDecider) Decide(_ context.Context, _ loop.Input) (loop.Decision, error) {
	return loop.Decision{Reply: d.reply}, nil
}

type twoStepToolDecider struct{}

func (d twoStepToolDecider) Decide(_ context.Context, in loop.Input) (loop.Decision, error) {
	if len(in.ToolResults) == 0 {
		payload, _ := json.Marshal(map[string]string{"text": "ping"})
		return loop.Decision{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: payload}}}, nil
	}
	return loop.Decision{Reply: "tool complete"}, nil
}

type slowDecider struct {
	delay time.Duration
}

func (d slowDecider) Decide(_ context.Context, _ loop.Input) (loop.Decision, error) {
	time.Sleep(d.delay)
	return loop.Decision{Reply: "done"}, nil
}

type errorDecider struct{}

func (d errorDecider) Decide(_ context.Context, _ loop.Input) (loop.Decision, error) {
	return loop.Decision{}, errors.New("decider failure")
}

type echoTool struct{}

func (echoTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{Name: "echo", Description: "Echo input"}
}

func (echoTool) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{ID: call.ID, Name: call.Name, Output: call.Input}, nil
}

func TestSessionStreamingLifecycle(t *testing.T) {
	s, err := New(Config{
		Decider: staticDecider{reply: "hello from session"},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	if err := s.Send(context.Background(), "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}

	updates := collectUntilRunEnd(t, s.Updates(), 2*time.Second)

	if !containsType(updates, UpdateRunStart) {
		t.Fatal("expected run_start update")
	}
	if !containsType(updates, UpdateRunEnd) {
		t.Fatal("expected run_end update")
	}

	messageEnd, ok := findEvent(updates, events.MessageEnd)
	if !ok {
		t.Fatal("expected message_end event")
	}
	if strings.TrimSpace(messageEnd.Content) != "hello from session" {
		t.Fatalf("expected message_end content, got %q", messageEnd.Content)
	}

	var streamed strings.Builder
	for _, update := range updates {
		if update.Type == UpdateEvent && update.Event.Type == events.MessageUpdate {
			streamed.WriteString(update.Event.Delta)
		}
	}
	if streamed.Len() != 0 {
		t.Fatalf("expected no synthetic message updates for non-streaming deciders, got %q", streamed.String())
	}
}

func TestSessionToolEvents(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(echoTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	s, err := New(Config{
		Decider:  twoStepToolDecider{},
		Executor: reg,
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	if err := s.Send(context.Background(), "run tool"); err != nil {
		t.Fatalf("send: %v", err)
	}
	updates := collectUntilRunEnd(t, s.Updates(), 2*time.Second)

	if _, ok := findEvent(updates, events.ToolStart); !ok {
		t.Fatal("expected tool start event")
	}
	if toolEnd, ok := findEvent(updates, events.ToolEnd); !ok {
		t.Fatal("expected tool end event")
	} else if toolEnd.ToolResult == nil || toolEnd.ToolResult.Name != "echo" {
		t.Fatalf("unexpected tool_end payload: %+v", toolEnd.ToolResult)
	}

	end := findRunEnd(t, updates)
	if end.Result == nil || end.Result.Reply != "tool complete" {
		t.Fatalf("expected final reply from second step, got %#v", end.Result)
	}
}

func TestSessionRejectsConcurrentRuns(t *testing.T) {
	s, err := New(Config{
		Decider: slowDecider{delay: 80 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	if err := s.Send(context.Background(), "first"); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if err := s.Send(context.Background(), "second"); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("expected in-progress error, got %v", err)
	}

	_ = collectUntilRunEnd(t, s.Updates(), 2*time.Second)
}

func TestSessionEmitsRunError(t *testing.T) {
	s, err := New(Config{
		Decider: errorDecider{},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	if err := s.Send(context.Background(), "boom"); err != nil {
		t.Fatalf("send: %v", err)
	}
	updates := collectUntilRunEnd(t, s.Updates(), 2*time.Second)
	if !containsType(updates, UpdateRunStart) {
		t.Fatal("expected run_start update")
	}
	if !containsType(updates, UpdateRunError) {
		t.Fatal("expected run_error update")
	}
}

func TestShouldDropUnderPressure(t *testing.T) {
	if !shouldDropUnderPressure(events.MessageUpdate) {
		t.Fatal("expected MessageUpdate to be drop-eligible")
	}
	if shouldDropUnderPressure(events.MessageEnd) {
		t.Fatal("expected MessageEnd to be preserved")
	}
	if shouldDropUnderPressure(events.ToolEnd) {
		t.Fatal("expected ToolEnd to be preserved")
	}
}

func collectUntilRunEnd(t *testing.T, ch <-chan Update, timeout time.Duration) []Update {
	t.Helper()
	deadline := time.After(timeout)
	updates := make([]Update, 0, 32)
	for {
		select {
		case update := <-ch:
			updates = append(updates, update)
			if update.Type == UpdateRunEnd || update.Type == UpdateRunError {
				return updates
			}
		case <-deadline:
			t.Fatalf("timeout waiting for session updates")
		}
	}
}

func containsType(updates []Update, want string) bool {
	for _, update := range updates {
		if update.Type == want {
			return true
		}
	}
	return false
}

func findEvent(updates []Update, eventType string) (events.Event, bool) {
	for _, update := range updates {
		if update.Type == UpdateEvent && update.Event.Type == eventType {
			return update.Event, true
		}
	}
	return events.Event{}, false
}

func findRunEnd(t *testing.T, updates []Update) Update {
	t.Helper()
	for _, update := range updates {
		if update.Type == UpdateRunEnd {
			return update
		}
	}
	t.Fatalf("missing run_end update")
	return Update{}
}
