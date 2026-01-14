# Plan: Agentic Weave

## Goals
- Extract a pluggable, LLM-agnostic agentic tool framework.
- Support advanced tool use patterns (search, examples, defer-load, allowed callers) without forcing complexity.
- Provide clear docs and runnable examples so teams can adopt quickly.

## Non-Goals (for v0)
- Shipping a full LLM orchestration layer.
- Owning persistence, scheduling, or UI integrations.
- Provider-specific features baked into the core.

## Complexity Levels (opt-in)
1) **Core Only** — Register tools, list tools, execute tool calls.
2) **Core + Policy** — Allowlist filtering, schema hashing, caller gating.
3) **Advanced Optional Modules** — Context, skills, MCP, tool search, defer-loading, batch execution.

## Advanced Tool Use Mapping
- Tool search → `ToolSearcher` (query-based discovery).
- Tool examples → `ToolDefinition.Examples` (input/output hints).
- Defer loading → `ToolDefinition.DeferLoad` + `ToolFetcher` (lazy load).
- Allowed callers → `ToolDefinition.AllowedCallers` + `ToolCall.Caller` (caller gating).
- Programmatic calls → direct `ToolExecutor.Execute` with `Caller` metadata.

## Plan Tasks (one line each)
1) Define core API types and interfaces (Tool, ToolExecutor, ToolDefinition, ToolCall).
2) Implement registry with policy + schema hash + caller gating.
3) Add executor helpers (composite, filtered, parallel batch).
4) Add tool search + defer-load interfaces.
5) Document advanced tool use feature mapping.
6) Write implementation guide (package layout, adapters, migration).
7) Write getting started guide with runnable example.
8) Provide streaming agent example (turn + message events) and verify `go test ./...`.
9) Add TASKS.md and AGENTS.md instructions for future agents.

## Deliverables
- `agentic` core package and `agentic/executor` helpers.
- Documentation set: `PLAN.md`, `IMPLEMENTATION.md`, `GETTING_STARTED.md`.
- Runnable streaming example: `examples/basic`.
- TASKS.md workflow instructions for future agents.
