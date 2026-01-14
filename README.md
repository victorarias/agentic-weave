# Agentic Weave

![Go](https://img.shields.io/badge/go-1.22%2B-blue)
![Status](https://img.shields.io/badge/status-early--stage-orange)

Pluggable, LLM-agnostic tooling framework for agentic systems.

## Quickstart
```go
import "github.com/victorarias/agentic-weave/agentic"

reg := agentic.NewRegistry()
reg.Register(MyTool{})

call := agentic.ToolCall{
  Name:  "my_tool",
  Input: mustJSON(MyInput{...}),
}

result, _ := reg.Execute(ctx, call)
```

Docs:
- `GETTING_STARTED.md`
- `PLAN.md`
- `IMPLEMENTATION.md`
- `TASKS.md`
