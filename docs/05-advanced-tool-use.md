# Advanced Tool Use

Agentic Weave supports advanced tool use patterns without forcing complexity.

## Tool Search
Expose a searcher to reduce tool list size.

```go
var searcher agentic.ToolSearcher = toolsearch.StaticSearcher{Tools: defs}
```

## Tool Examples
Populate `ToolDefinition.Examples` to reduce input errors.

## Defer Loading
Mark tools with `DeferLoad` and resolve them on demand via `ToolFetcher`.

## Allowed Callers
Gate tool execution by caller type (e.g., "llm", "programmatic", "code_execution").

## Programmatic Tool Calling
Call tools directly from application code and skip LLM selection.

## Batch Execution (ParallelExecutor)
Run multiple tool calls concurrently using `executor.ParallelExecutor`.

Notes:
- `ExecuteBatch` returns results in the same order as the input calls.
- If any call fails, it returns a `BatchError` with `Errors[i]` aligned to `calls[i]`.
- When context is canceled, `ExecuteBatch` returns early with the context error.

```go
batch := executor.NewParallel(registry, nil)
calls := []agentic.ToolCall{{Name: "a"}, {Name: "b"}}
results, err := batch.ExecuteBatch(ctx, calls)
if err != nil {
  if batchErr, ok := err.(executor.BatchError); ok {
    // batchErr.Errors[i] matches calls[i]
  }
}
_ = results
```
