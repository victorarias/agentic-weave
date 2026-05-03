# Provider-neutral streaming helpers

The `agentic/providers` package includes small orchestration helpers for code that
wants to stay provider-agnostic while still using streamed model output.

These helpers sit **above** the raw `providers.Streamer` interface and **below**
higher-level app code:

- `providers.Decide(...)` — stream one request and collect a full reply/tool-call result
- `providers.CollectDecision(...)` — collect an existing stream of provider events
- `providers.NewStreamingLoopDecider(...)` — adapt a `providers.Streamer` to `loop.Decider`
- `providers.NewStreamingCompactor(...)` — adapt a `providers.Streamer` to `budget.Compactor`

This keeps application repositories from re-implementing:

- stream collection
- retry-on-transient-stream-failure behavior
- stop-reason normalization
- budget compaction plumbing

## Collect a streamed provider response

```go
streamer, err := openai.New(openai.Config{
    APIKey:         os.Getenv("OPENAI_API_KEY"),
    Model:          "gpt-5",
    MaxTokensField: openai.MaxTokensFieldCompletion, // OpenAI reasoning models reject "max_tokens"
})
if err != nil {
    log.Fatal(err)
}

decision, err := providers.Decide(ctx, streamer, providers.Input{
    SystemPrompt: "You are helpful.",
    UserMessage:  "Say hello.",
})
if err != nil {
    log.Fatal(err)
}

fmt.Println(decision.Reply)
```

`providers.Input.UserInlineData` carries images for the current user message.
Provider adapters serialize those bytes into the provider's native vision format;
historical user-message images are carried on `message.AgentMessage.InlineData`.

## Use a provider in the generic agent loop

```go
decider := providers.NewStreamingLoopDecider(streamer, func(delta string) {
    fmt.Print(delta)
})

runner := loop.New(loop.Config{
    Decider: decider,
})
```

The loop decider retries transient stream failures before any output is emitted.
Once text or tool calls have started streaming, failures are returned directly so
callers do not accidentally replay a partially visible response.

When used with `loop.Runner`, the current user turn is already present in
history, including `InlineData`. `NewStreamingLoopDecider` therefore forwards
images through history and does not duplicate them through
`providers.Input.UserInlineData`.

## Use a provider for context compaction

```go
compactor := providers.NewStreamingCompactor(
    streamer,
    providers.StaticCompactionPromptProfile{
        SystemPrompt: providers.DefaultCompactionSystemPrompt,
    },
)
```

## Design notes

- Helpers intentionally rely only on `providers.Streamer` and `providers.Input`.
- Provider SDK-specific request shapes stay inside provider packages like
  `agentic/providers/anthropic` and `agentic/providers/openai`.
- Application code can switch providers without rewriting loop-decider or
  compaction glue.

## openai.Config.MaxTokensField

The OpenAI Chat Completions wire shape carries two field names that mean the
same thing in practice, and which one a model accepts is per-model:

- **`MaxTokensFieldLegacy`** (`max_tokens`) — required by every non-OpenAI
  provider that mirrors the OpenAI wire (DeepSeek native and via OpenRouter,
  Kimi, Qwen, Anthropic-via-OpenRouter, ...). These advertise `max_tokens` in
  their `supported_parameters` and reject `max_completion_tokens` outright when
  OpenRouter's `provider.require_parameters` is set.
- **`MaxTokensFieldCompletion`** (`max_completion_tokens`) — required by
  OpenAI o1/o3/GPT-5+ reasoning models, which conversely reject `max_tokens`.

`openai.New(...)` rejects an empty or unknown value at construction time so
the misconfiguration surfaces synchronously rather than as an opaque 4xx at
request time.
