package providers

import "context"

// Streamer is the provider-agnostic streaming interface.
// Both anthropic.Client and openai.Client implement it.
type Streamer interface {
	Stream(ctx context.Context, input Input) (<-chan StreamEvent, error)
}
