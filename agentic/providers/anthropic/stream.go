package anthropic

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/usage"
	"github.com/victorarias/agentic-weave/capabilities"
)

// StreamEvent represents a streaming update from Claude.
type StreamEvent interface {
	streamEvent()
}

// TextDeltaEvent represents incremental text.
type TextDeltaEvent struct {
	Delta string
}

func (TextDeltaEvent) streamEvent() {}

// ToolCallEvent represents the model invoking a tool.
type ToolCallEvent struct {
	Call agentic.ToolCall
}

func (ToolCallEvent) streamEvent() {}

// DoneEvent signals streaming completion.
type DoneEvent struct {
	StopReason           string
	StopReasonNormalized usage.StopReason
	Usage                *usage.Usage
}

func (DoneEvent) streamEvent() {}

// ErrorEvent signals a streaming error.
type ErrorEvent struct {
	Err error
}

func (ErrorEvent) streamEvent() {}

// Stream calls Anthropic Messages API with streaming enabled.
func (c *Client) Stream(ctx context.Context, input Input) (<-chan StreamEvent, error) {
	req := buildRequest(c, input)
	stream := c.client.Messages.NewStreaming(ctx, req)

	events := make(chan StreamEvent)
	go func() {
		defer close(events)
		defer func() { _ = stream.Close() }()

		state := newStreamState()
		debug := os.Getenv("ANTHROPIC_STREAM_DEBUG") != ""
		for stream.Next() {
			event := stream.Current()
			if debug {
				log.Printf("anthropic stream event: type=%s raw=%s", event.Type, event.RawJSON())
			}
			for _, ev := range state.handle(event) {
				events <- ev
			}
		}

		if err := stream.Err(); err != nil {
			events <- ErrorEvent{Err: err}
			return
		}

		events <- state.done()
	}()

	return events, nil
}

type streamState struct {
	toolCounter int
	tools       map[int64]*toolState
	stopReason  string
	usage       *usage.Usage
}

type toolState struct {
	id          string
	name        string
	partialJSON strings.Builder
	sawDelta    bool
}

func newStreamState() *streamState {
	return &streamState{
		tools: make(map[int64]*toolState),
	}
}

func (s *streamState) handle(event sdk.MessageStreamEventUnion) []StreamEvent {
	switch evt := event.AsAny().(type) {
	case sdk.ContentBlockStartEvent:
		return s.handleContentBlockStart(evt)
	case sdk.ContentBlockDeltaEvent:
		return s.handleContentBlockDelta(evt)
	case sdk.ContentBlockStopEvent:
		return s.handleContentBlockStop(evt)
	case sdk.MessageDeltaEvent:
		stopReason := strings.TrimSpace(string(evt.Delta.StopReason))
		if stopReason != "" {
			s.stopReason = stopReason
		}
		usageValue := capabilities.NormalizeUsage(int(evt.Usage.InputTokens), int(evt.Usage.OutputTokens), 0)
		s.usage = &usageValue
	}
	return nil
}

func (s *streamState) handleContentBlockStart(evt sdk.ContentBlockStartEvent) []StreamEvent {
	if evt.ContentBlock.Type == "text" && evt.ContentBlock.Text != "" {
		return []StreamEvent{TextDeltaEvent{Delta: evt.ContentBlock.Text}}
	}
	if evt.ContentBlock.Type != "tool_use" {
		return nil
	}
	name := strings.TrimSpace(evt.ContentBlock.Name)
	if name == "" {
		name = "tool"
	}
	id := ensureToolCallID(evt.ContentBlock.ID, name, s.toolCounter)
	s.toolCounter++

	state := &toolState{
		id:   id,
		name: name,
	}
	if evt.ContentBlock.Input != nil {
		if raw, err := json.Marshal(evt.ContentBlock.Input); err == nil {
			state.partialJSON.Write(raw)
		}
	}
	s.tools[evt.Index] = state
	return nil
}

func (s *streamState) handleContentBlockDelta(evt sdk.ContentBlockDeltaEvent) []StreamEvent {
	switch evt.Delta.Type {
	case "text_delta":
		return []StreamEvent{TextDeltaEvent{Delta: evt.Delta.Text}}
	case "input_json_delta":
		if state := s.tools[evt.Index]; state != nil {
			if !state.sawDelta {
				state.partialJSON.Reset()
				state.sawDelta = true
			}
			state.partialJSON.WriteString(evt.Delta.PartialJSON)
		}
	}
	return nil
}

func (s *streamState) handleContentBlockStop(evt sdk.ContentBlockStopEvent) []StreamEvent {
	state := s.tools[evt.Index]
	if state == nil {
		return nil
	}
	delete(s.tools, evt.Index)

	call := agentic.ToolCall{
		ID:    state.id,
		Name:  state.name,
		Input: parseToolInput(state.partialJSON.String()),
	}
	return []StreamEvent{ToolCallEvent{Call: call}}
}

func (s *streamState) done() DoneEvent {
	stopReason := strings.TrimSpace(s.stopReason)
	if stopReason == "" {
		stopReason = "end_turn"
	}
	if s.usage == nil {
		usageValue := capabilities.NormalizeUsage(0, 0, 0)
		s.usage = &usageValue
	}
	return DoneEvent{
		StopReason:           stopReason,
		StopReasonNormalized: capabilities.StopReasonFromFinish(stopReason),
		Usage:                s.usage,
	}
}

func parseToolInput(raw string) json.RawMessage {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		log.Printf("ERROR anthropic stream: empty tool input")
		return json.RawMessage(`""`)
	}
	if !json.Valid([]byte(trimmed)) {
		log.Printf("ERROR anthropic stream: invalid tool input json: %s", summarizeJSON(trimmed))
		return json.RawMessage(trimmed)
	}
	return json.RawMessage(trimmed)
}

func summarizeJSON(raw string) string {
	const limit = 256
	if len(raw) <= limit {
		return raw
	}
	return raw[:limit] + "...(truncated)"
}
