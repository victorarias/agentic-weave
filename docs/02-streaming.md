# Streaming Events

Agentic Weave streams agent events for responsive UIs.

## Event Types
- `agent_start`, `agent_end`
- `turn_start`, `turn_end`
- `message_start`, `message_update`, `message_end`
- `tool_execution_start`, `tool_execution_end`

## Message IDs and Deltas
Use `MessageID` and `Delta` to render partial outputs safely.

```go
agent.StreamMessage(ctx, "add 10 and 32", events.SinkFunc(func(e events.Event) {
  switch e.Type {
  case events.MessageUpdate:
    fmt.Print(e.Delta)
  case events.MessageEnd:
    fmt.Println()
  }
}))
```

## Turn Boundaries
Turns group one LLM response and its tool calls. Use turn events to separate UI sections or logs.
