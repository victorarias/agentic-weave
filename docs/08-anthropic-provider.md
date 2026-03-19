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
- `ANTHROPIC_CACHE_TTL` (optional; `5m` or `1h`)
- `ANTHROPIC_CACHE_MODE` (optional; `automatic`, `explicit`, `hybrid`, or `disabled`)

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

The provider also supports streaming via `client.Stream(...)`, which returns a
channel of `anthropic.StreamEvent` values (text deltas, tool calls, and a final
done event).

```go
events, err := client.Stream(ctx, anthropic.Input{
    SystemPrompt: "You are a helpful assistant.",
    UserMessage:  "Summarize the latest changes.",
    Tools:        tools,
})
if err != nil {
    // handle request error
}

decision, err := anthropic.CollectDecision(events)
if err != nil {
    // handle stream error
}

fmt.Println(decision.Reply)
```

## Notes

- Tool calls are returned as `agentic.ToolCall` values with raw JSON input.
- Tool results should be provided via `History` as `message.AgentMessage` entries.

## History Compaction

The provider also exposes a reusable streaming compactor that implements
`budget.Compactor`:

```go
client, err := anthropic.NewFromEnv()
if err != nil {
    // handle config error
}

compactor := anthropic.NewStreamingCompactor(
    client,
    anthropic.StaticCompactionPromptProfile{
        SystemPrompt: anthropic.DefaultCompactionSystemPrompt,
    },
)
```

Use this when wiring `budget.Manager` so compaction can reuse the same Anthropic
streaming client while still keeping a purpose-specific compaction prompt.

## Prompt Caching

Anthropic prompt caching supports multiple modes via `Config.CacheMode` or `ANTHROPIC_CACHE_MODE`:

- `CacheModeAutomatic`: top-level automatic caching that advances to the last cacheable block.
- `CacheModeExplicit`: explicit block-level cache control on the last system block, last tool, and last block in the final message.
- `CacheModeExplicitStablePrefixWithAutomatic` (`hybrid`): explicit stable-prefix breakpoints on the last system block, last tool, and last block in the penultimate message, plus top-level automatic caching for the moving tail. This is useful when the final message is transient but you still want an explicit fallback breakpoint before it.
- `CacheModeDisabled` (`disabled`/`off`): disable prompt caching entirely.
