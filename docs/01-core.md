# Core Concepts

## Tools
A tool has a definition and an execute method.

```go
func (MyTool) Definition() agentic.ToolDefinition { ... }
func (MyTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) { ... }
```

## Registry
Register tools and execute tool calls.

```go
reg := agentic.NewRegistry()
if err := reg.Register(MyTool{}); err != nil {
  // handle error
}
result, _ := reg.Execute(ctx, agentic.ToolCall{Name: "my_tool", Input: payload})
```

## Tool Calls
Tool calls carry:
- `Name` — tool identifier
- `Input` — JSON payload
- `SchemaHash` — optional schema handshake
- `Caller` — optional call source

## Policy
Use policies to allowlist tools or block callers.

```go
reg := agentic.NewRegistry(agentic.WithPolicy(agentic.NewAllowlistPolicy([]string{"my_tool"})))
```
