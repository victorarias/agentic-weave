package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/usage"
	"github.com/victorarias/agentic-weave/capabilities"
)

// StreamEvent represents a single streaming event emitted by Stream.
// Consumers can reconstruct a final Decision by concatenating TextDeltaEvent
// deltas and collecting ToolUseEvent calls until DoneEvent is received.
type StreamEvent interface {
	anthropicStreamEvent()
}

// TextDeltaEvent represents incremental text from the model.
type TextDeltaEvent struct {
	Delta string
}

func (TextDeltaEvent) anthropicStreamEvent() {}

// ToolUseEvent represents a fully-formed tool call requested by the model.
type ToolUseEvent struct {
	Call agentic.ToolCall
}

func (ToolUseEvent) anthropicStreamEvent() {}

// DoneEvent signals completion of the stream.
type DoneEvent struct {
	StopReason string
	Usage      *usage.Usage
}

func (DoneEvent) anthropicStreamEvent() {}

// ErrorEvent signals a stream error. If received, the stream will end shortly after.
type ErrorEvent struct {
	Err error
}

func (ErrorEvent) anthropicStreamEvent() {}

// Stream calls the Anthropic Messages API in streaming mode.
//
// History is provided as agentic/message.AgentMessage values (including role=tool),
// and is converted to Anthropic's required tool_use/tool_result structure (tool
// results are sent as a user message containing tool_result blocks).
func (c *Client) Stream(ctx context.Context, input Input) (<-chan StreamEvent, error) {
	messages := appendHistory(nil, input.History)

	userMessage := strings.TrimSpace(input.UserMessage)
	if userMessage != "" {
		messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(userMessage)))
	}

	req := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(c.maxTokens),
		Messages:  messages,
	}

	if len(input.Tools) > 0 {
		req.Tools = toolDefsToAnthropic(input.Tools)
	}

	if system := strings.TrimSpace(input.SystemPrompt); system != "" {
		req.System = []anthropic.TextBlockParam{{Text: system}}
	}

	if input.MaxTokens > 0 {
		req.MaxTokens = int64(input.MaxTokens)
	}

	temperature := input.Temperature
	if temperature == nil {
		temperature = c.temperature
	}
	if temperature != nil {
		req.Temperature = anthropic.Float(*temperature)
	}

	stream := c.client.Messages.NewStreaming(ctx, req)

	events := make(chan StreamEvent, 32)
	go func() {
		defer close(events)
		defer func() { _ = stream.Close() }()

		var (
			stopReason string
			usageValue *usage.Usage
		)

		type toolState struct {
			id          string
			name        string
			partialJSON strings.Builder
		}
		var currentTool *toolState

		for stream.Next() {
			ev := stream.Current()

			switch ev.Type {
			case "content_block_start":
				if ev.ContentBlock.Type != "tool_use" {
					continue
				}
				currentTool = &toolState{
					id:   strings.TrimSpace(ev.ContentBlock.ID),
					name: strings.TrimSpace(ev.ContentBlock.Name),
				}

			case "content_block_delta":
				switch ev.Delta.Type {
				case "text_delta":
					if ev.Delta.Text != "" {
						events <- TextDeltaEvent{Delta: ev.Delta.Text}
					}
				case "input_json_delta":
					if currentTool != nil && ev.Delta.PartialJSON != "" {
						currentTool.partialJSON.WriteString(ev.Delta.PartialJSON)
					}
				}

			case "content_block_stop":
				if currentTool == nil {
					continue
				}

				rawJSON := strings.TrimSpace(currentTool.partialJSON.String())
				if rawJSON == "" {
					rawJSON = "{}"
				}
				if !json.Valid([]byte(rawJSON)) {
					events <- ErrorEvent{Err: fmt.Errorf("anthropic stream: invalid tool input json for %q (%s): %q", currentTool.name, currentTool.id, rawJSON)}
					return
				}

				callID := currentTool.id
				if callID == "" {
					// Anthropic requires an id, but this keeps our downstream logic consistent.
					callID = currentTool.name
				}
				events <- ToolUseEvent{Call: agentic.ToolCall{
					ID:    callID,
					Name:  currentTool.name,
					Input: json.RawMessage(rawJSON),
				}}
				currentTool = nil

			case "message_delta":
				stopReason = string(ev.Delta.StopReason)
				u := capabilities.NormalizeUsage(int(ev.Usage.InputTokens), int(ev.Usage.OutputTokens), 0)
				usageValue = &u
			}
		}

		if err := stream.Err(); err != nil {
			events <- ErrorEvent{Err: fmt.Errorf("anthropic stream: %w", err)}
			return
		}

		if stopReason == "" {
			stopReason = "end_turn"
		}
		events <- DoneEvent{StopReason: stopReason, Usage: usageValue}
	}()

	return events, nil
}

// CollectDecision converts Stream events into a Decision.
// It returns an error if an ErrorEvent is received or the stream ends without DoneEvent.
func CollectDecision(events <-chan StreamEvent) (Decision, error) {
	if events == nil {
		return Decision{}, errors.New("anthropic stream: nil events channel")
	}

	var (
		reply strings.Builder
		calls []agentic.ToolCall

		stop string
		u    *usage.Usage
	)

	for ev := range events {
		switch e := ev.(type) {
		case TextDeltaEvent:
			reply.WriteString(e.Delta)
		case ToolUseEvent:
			calls = append(calls, e.Call)
		case DoneEvent:
			stop = e.StopReason
			u = e.Usage
			return Decision{
				Reply:      strings.TrimSpace(reply.String()),
				ToolCalls:  calls,
				StopReason: stop,
				Usage:      u,
			}, nil
		case ErrorEvent:
			if e.Err == nil {
				return Decision{}, errors.New("anthropic stream failed")
			}
			return Decision{}, e.Err
		}
	}

	return Decision{}, errors.New("anthropic stream ended without done event")
}
