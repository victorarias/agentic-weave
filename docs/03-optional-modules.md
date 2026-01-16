# Optional Modules

## Schema
- `schema.HashJSON` for stable schema hashing.
- `schema.SchemaFromStruct` for JSON schema from Go structs.

## Skills
- `skills.Source` to load skills from file or DB.
- File loader reads markdown with optional frontmatter.

## Context
- `context.Manager` compacts messages using a token counter + compaction hook.
- `context/budget` adds token-budget compaction with reserve/keep policies.

## Loop
- `loop.Runner` provides a mono-like tool loop with compaction, truncation, and events.

## MCP
- `mcp.Registry` wraps an MCP client and gates by allowlist.

## Events
- `events.Event` and `events.Sink` for streaming agent updates.

## Limits & Usage
- `limits.ModelLimits` and `limits.Provider` expose context window + max output.
- `usage.Usage` and `usage.StopReason` standardize token usage reporting.

## Truncation
- `truncate.Head` / `truncate.Tail` provide safe tool output truncation.

## History
- `history.Store` is an optional persistence hook; `history.Rewriter` supports compaction replaces.
