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
