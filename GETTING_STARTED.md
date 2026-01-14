# Getting Started

This guide assumes basic Go familiarity.

## Quick Run (streaming agent example)
```
go run ./examples/basic
```
Expected output:
```
The sum is 42.
```

## Provider Examples (mocked loops)
```
go run ./examples/anthropic
```
```
go run ./examples/gemini
```
These examples show tool-call loops and message shaping without hitting real APIs.

## Provider Examples (real SDKs)
```
cd examples/anthropic-real

go run .
```
```
cd examples/gemini-real

go run .
```
These examples live in nested Go modules so the core library stays SDK-free.

## Minimal Usage
```go
reg := agentic.NewRegistry()
if err := reg.Register(MyTool{}); err != nil {
  // handle error
}

call := agentic.ToolCall{
  Name:  "my_tool",
  Input: mustJSON(MyInput{...}),
}

result, err := reg.Execute(ctx, call)
```

## Define a Tool
```go
type HelloTool struct{}

func (HelloTool) Definition() agentic.ToolDefinition {
  return agentic.ToolDefinition{
    Name:        "hello",
    Description: "Greets a name",
    Examples: []agentic.ToolExample{
      {Input: mustJSON(map[string]string{"name": "Ada"})},
    },
  }
}

func (HelloTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
  var input struct{ Name string `json:"name"` }
  if err := json.Unmarshal(call.Input, &input); err != nil {
    return agentic.ToolResult{Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
  }
  return agentic.ToolResult{Name: call.Name, Output: mustJSON(map[string]string{"message": "hi " + input.Name})}, nil
}
```

## Build a Tiny Streaming Agent (rule-based)
```go
agent := NewAgent(reg)
agent.StreamMessage(ctx, "add 10 and 32", events.SinkFunc(func(e events.Event) {
  if e.Type == events.MessageUpdate {
    fmt.Print(e.Delta)
  }
}))
```

## When You Need Advanced Features
- **Allowed callers**: set `AllowedCallers` in `ToolDefinition` and include `Caller` in `ToolCall`.
- **Tool examples**: fill `Examples` in `ToolDefinition` to reduce parameter errors.
- **Tool search**: implement `ToolSearcher` in a provider adapter to query tools.
- **Defer loading**: set `DeferLoad` and provide a `ToolFetcher`.

## Next Steps
- Read `PLAN.md` for roadmap and complexity levels.
- Read `IMPLEMENTATION.md` for interface details and adapter guidance.
