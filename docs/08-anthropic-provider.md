# Anthropic Claude Provider (Optional)

This provider calls the Anthropic Messages API via the official Go SDK.

## Package

- `agentic/providers/anthropic`

## Environment Variables

- `ANTHROPIC_API_KEY` (required)
- `ANTHROPIC_MODEL` (required)
- `ANTHROPIC_BASE_URL` (optional, default: Anthropic SDK default)
- `ANTHROPIC_MAX_TOKENS` (optional)
- `ANTHROPIC_TEMPERATURE` (optional)

## Usage

```go
client, err := anthropic.NewFromEnv()
if err != nil {
    // handle config error
}

result, err := client.Decide(ctx, anthropic.Input{
    SystemPrompt: "You are a helpful assistant.",
    UserMessage:  "Summarize the latest changes.",
    Tools:        tools,
})
if err != nil {
    // handle request error
}

fmt.Println(result.Reply)
```

## Streaming

```go
stream, err := client.Stream(ctx, anthropic.Input{
    SystemPrompt: "Use tools when needed.",
    UserMessage:  "Add 10 and 32.",
    Tools:        tools,
})
if err != nil {
    // handle request error
}

for event := range stream {
    switch e := event.(type) {
    case anthropic.TextDeltaEvent:
        fmt.Print(e.Delta)
    case anthropic.ToolCallEvent:
        // execute tool using e.Call
    case anthropic.DoneEvent:
        fmt.Printf("\n(stop=%s)\n", e.StopReason)
    case anthropic.ErrorEvent:
        // handle stream error
    }
}
```

## Notes

- Tool calls are returned as `agentic.ToolCall` values with raw JSON input.
- Tool results should be provided via `History` as `message.AgentMessage` entries.
- Streaming emits tool call events after assembling JSON input deltas.
- Normalized stop reasons are available on `Decision.StopReasonNormalized` and `DoneEvent.StopReasonNormalized`.
