// Package providers defines the provider-agnostic LLM streaming interface.
//
// Both the Anthropic and OpenAI providers implement Streamer so consumers
// can switch between providers without changing their orchestration code.
package providers

import (
	"context"
	"encoding/json"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

// Input represents a provider-agnostic LLM request.
type Input struct {
	SystemPrompt   string
	UserMessage    string
	History        []message.AgentMessage
	Tools          []agentic.ToolDefinition
	UserInlineData []agentic.InlineData
	MaxTokens      int
	Temperature    *float64

	// OutputJSONSchema, if set, instructs the model to produce structured JSON output.
	OutputJSONSchema json.RawMessage

	// Labels are arbitrary key-value pairs attached to the API request for
	// cost attribution, filtering, and tracking. Provider support varies:
	//   - Vertex AI: sent as "labels" in the request body (up to 64 pairs)
	//   - OpenAI: sent as "metadata" on chat completions (up to 16 pairs)
	//   - Anthropic: not yet supported by the API; field is accepted but ignored
	Labels map[string]string

	// Hook receives provider-boundary events. Providers may attach the exact
	// provider-specific request JSON after they finish materializing it.
	Hook ProviderHook
}

// ProviderHook receives provider-boundary request/response events.
type ProviderHook interface {
	BeforeProviderRequest(ctx context.Context, event ProviderRequestEvent)
	AfterProviderResponse(ctx context.Context, event ProviderResponseEvent)
	OnProviderError(ctx context.Context, event ProviderErrorEvent)
}

// ProviderRequestEvent describes the fully materialized provider request.
type ProviderRequestEvent struct {
	Provider    string
	Model       string
	Operation   string
	RequestJSON []byte
}

// ProviderResponseEvent describes a completed provider response.
type ProviderResponseEvent struct {
	Provider     string
	Model        string
	Operation    string
	ResponseJSON []byte
	StopReason   string
	Usage        *usage.Usage
}

// ProviderErrorEvent describes a provider request/response failure.
type ProviderErrorEvent struct {
	Provider    string
	Model       string
	Operation   string
	RequestJSON []byte
	Err         error
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

// ReasoningDeltaEvent represents an incremental reasoning trace from the model.
//
// Different providers serialize reasoning differently:
//   - DeepSeek (direct or via OpenRouter): "reasoning_content"
//   - OpenRouter (most other reasoning models): "reasoning"
//   - Some adapters: "reasoning_text"
//
// Provider clients normalize the textual delta into Delta. Format identifies
// the wire field they read it from so callers that need to round-trip the
// reasoning trace back to the same provider can preserve fidelity. Raw is
// the verbatim provider blob from the streaming delta when callers want to
// persist it for replay (e.g. DeepSeek requires the prior reasoning_content
// on multi-turn calls or the request 400s).
type ReasoningDeltaEvent struct {
	Delta  string
	Format string
	Raw    json.RawMessage
}

func (ReasoningDeltaEvent) providerStreamEvent() {}

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
