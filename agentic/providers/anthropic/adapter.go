package anthropic

import (
	"context"

	"github.com/victorarias/agentic-weave/agentic/providers"
)

// StreamerAdapter wraps an Anthropic *Client so it satisfies providers.Streamer.
// It converts providers.Input → anthropic.Input and
// anthropic.StreamEvent → providers.StreamEvent.
type StreamerAdapter struct {
	Client *Client
}

// NewStreamerAdapter creates a providers.Streamer backed by an Anthropic client.
func NewStreamerAdapter(client *Client) *StreamerAdapter {
	return &StreamerAdapter{Client: client}
}

func (a *StreamerAdapter) Stream(ctx context.Context, input providers.Input) (<-chan providers.StreamEvent, error) {
	anthropicInput := Input{
		SystemPrompt:     input.SystemPrompt,
		UserMessage:      input.UserMessage,
		History:          input.History,
		Tools:            input.Tools,
		MaxTokens:        input.MaxTokens,
		Temperature:      input.Temperature,
		OutputJSONSchema: input.OutputJSONSchema,
		Hook:             input.Hook,
	}

	anthropicEvents, err := a.Client.Stream(ctx, anthropicInput)
	if err != nil {
		return nil, err
	}

	out := make(chan providers.StreamEvent, 32)
	go func() {
		defer close(out)
		for ev := range anthropicEvents {
			switch e := ev.(type) {
			case TextDeltaEvent:
				out <- providers.TextDeltaEvent{Delta: e.Delta}
			case ToolUseEvent:
				out <- providers.ToolUseEvent{Call: e.Call}
			case DoneEvent:
				out <- providers.DoneEvent{StopReason: e.StopReason, Usage: e.Usage}
			case ErrorEvent:
				out <- providers.ErrorEvent{Err: e.Err}
			}
		}
	}()
	return out, nil
}
