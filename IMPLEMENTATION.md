# Implementation Guide

This document describes how to implement and extend Agentic Weave.

## Package Layout (proposed)
```
agentic-weave/
  agentic/
    tool.go           # Core types and interfaces
    registry.go       # Tool registry and execution
    policy.go         # Allowlist and policy helpers
    errors.go         # Shared errors
    schema/           # Optional: schema helpers
    events/           # Optional: event types + sinks
    executor/
      composite.go
      filtered.go
      parallel.go
  context/            # Optional: token counting + compaction hooks
  skills/             # Optional: skills loader + SkillSource
  mcp/                # Optional: MCP discovery + allowlist
  adapters/           # Optional: Anthropic/Gemini adapters
  examples/basic/
```

## Core Interfaces
### Tool
A tool is a self-contained action with a JSON input/output.
```
Definition() ToolDefinition
Execute(ctx, call) (ToolResult, error)
```

### ToolDefinition
- `Name`, `Description` are required.
- `InputSchema` and `SchemaHash` allow schema validation.
- `Examples` allow provider tool-use examples.
- `AllowedCallers` gates programmatic or tool-initiated calls.
- `DeferLoad` indicates tool should be fetched lazily.

### ToolExecutor
- `ListTools(ctx)`
- `Execute(ctx, call)`

### Optional Interfaces
- `ToolSearcher`: `SearchTools(ctx, query)` for tool search.
- `ToolFetcher`: `FetchTool(ctx, name)` for defer loading.
- `Policy`: allowlist + caller gating.

## Execution Flow
1) Host lists tools (for LLM context or UI).
2) Host or LLM creates `ToolCall`.
3) Executor validates policy, caller, and schema hash.
4) Tool executes, returning `ToolResult`.
5) Host decides how to feed results into the LLM (or not).

## Advanced Tool Use Support
### Tool Search Tool
- Add a `ToolSearcher` implementation that ranks tools by query.
- Host can invoke it via a dedicated tool call or out-of-band.

### Tool Use Examples
- Populate `ToolDefinition.Examples` with JSON inputs/outputs.
- Providers that support tool examples can render them; others ignore.

### Defer Loading
- Set `ToolDefinition.DeferLoad = true` for heavy or infrequently used tools.
- On call, use `ToolFetcher` to load the tool on demand.

### Allowed Callers / Programmatic Tool Calling
- Use `ToolCall.Caller` for explicit call sources.
- Executor enforces `AllowedCallers` if set.

## Optional Modules
### Schema
- `schema.HashJSON` for stable schema hashing.
- `schema.SchemaFromStruct` for basic JSON schema generation from structs.

### Events
- `events.Event` and `events.Sink` for streaming agent UI updates.
- Turn-level and message-level events mirror common UI needs.

### Skills
- `skills.Source` loads skills from file/DB.
- File source reads markdown with optional frontmatter.
- DB source is adapter-based for host apps.

### Context
- `context.Manager` compacts messages with token limits.
- Uses `TokenCounter` + `CompactionFunc` (host-supplied).

### MCP
- `mcp.Registry` wraps MCP client tools with allowlist gating.

### Adapters
- `adapters` provides capability flags and provider stubs.
- Host apps implement the actual LLM calls.

## LLM Adapter Responsibilities (Host)
- Map tool definitions into provider-specific formats.
- Enforce provider ordering (tool_use -> tool_result).
- Choose tool choice mode (`auto|none|tool`).
- Decide if tool results enter model context or stay local.

## Migration from Exsin (checklist)
1) Extract `agentic` core and `executor` helpers into this module.
2) Swap Exsin imports to `github.com/victorarias/agentic-weave/agentic`.
3) Move optional helpers (schema, events, skills, context, mcp) as needed.
4) Keep gRPC adapters and persistence in Exsin.
5) Update tests to use the new registry/executor types.
6) Validate tool schema handshake still rejects mismatches.

## Testing
- Unit test registry and executor behavior.
- Example app serves as a smoke test (`go run ./examples/basic`).
