// Package providers defines the provider-agnostic LLM streaming interface.
//
// Both the Anthropic and OpenAI providers implement Streamer so consumers
// can switch between providers without changing their orchestration code.
package providers

import (
	"encoding/json"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

// Input represents a provider-agnostic LLM request.
type Input struct {
	SystemPrompt string
	UserMessage  string
	History      []message.AgentMessage
	Tools        []agentic.ToolDefinition
	MaxTokens    int
	Temperature  *float64

	// OutputJSONSchema, if set, instructs the model to produce structured JSON output.
	OutputJSONSchema json.RawMessage
}

// StreamEvent is emitted by Streamer.Stream.
// Consumers reconstruct a complete response by concatenating TextDeltaEvent
// deltas and collecting ToolUseEvent calls until DoneEvent is received.
type StreamEvent interface {
	providerStreamEvent()
}

// TextDeltaEvent represents incremental text from the model.
type TextDeltaEvent struct{ Delta string }

func (TextDeltaEvent) providerStreamEvent() {}

// ToolUseEvent represents a fully-formed tool call requested by the model.
type ToolUseEvent struct{ Call agentic.ToolCall }

func (ToolUseEvent) providerStreamEvent() {}

// DoneEvent signals stream completion.
type DoneEvent struct {
	StopReason string
	Usage      *usage.Usage
}

func (DoneEvent) providerStreamEvent() {}

// ErrorEvent signals a stream error.
type ErrorEvent struct{ Err error }

func (ErrorEvent) providerStreamEvent() {}
