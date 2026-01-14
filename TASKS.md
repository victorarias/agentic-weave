# Agentic Weave

This file tracks current work items and progress.

## Current Initiative: initial-extraction
- [x] Define public API and package layout (core + optional submodules).
- [x] Document advanced tool use support (tool search, examples, defer-load, allowed callers).
- [x] Provide runnable example and ensure `go test ./...` passes.
- [x] Draft migration notes for extracting from Exsin.

## Implementation Tasks (small chunks)
- [x] Create repo scaffolding (`AGENTS.md`, `TASKS.md`, `go.mod`).
- [x] Add core types (`Tool`, `ToolCall`, `ToolResult`, `ToolDefinition`).
- [x] Implement registry with policy + schema hash + caller gating.
- [x] Add policy helpers (allow-all + allowlist).
- [x] Add executor helpers (composite, filtered, parallel batch).
- [x] Add tool search + defer-load interfaces.
- [x] Add schema hashing helper (optional module).
- [x] Add event types + EventSink interface (optional module).
- [ ] Add typed-tool schema generation helper (optional module).
- [ ] Add skills module with `SkillSource` and file loader.
- [ ] Add DB-backed skills loader (stub + interface).
- [ ] Add context module (token counter interface + compaction hook).
- [ ] Add MCP module (registry + allowlist policy).
- [ ] Add provider adapter stubs (Anthropic, Gemini) with capability flags.
- [ ] Add tool search example adapter (stubbed search scoring).
- [ ] Add defer-load example adapter (lazy fetch stub).
- [ ] Add tests for registry policy edge cases (schema hash, caller gating).
- [ ] Add tests for executors (composite, filtered, parallel).
- [ ] Add documentation for optional modules and adapter responsibilities.
- [ ] Add detailed extraction/migration checklist from Exsin.
- [ ] Add README quickstart snippets + badges.

## Progress Log
- 2026-01-14 10:18: Created repository structure, AGENTS.md, and TASKS.md.
- 2026-01-14 11:08: Added core module skeleton, docs, and runnable example; `go test ./...` and example run pass.
- 2026-01-14 11:11: Updated module path to github.com/victorarias/agentic-weave and verified `go test ./...`.
- 2026-01-14 11:12: Updated PLAN.md with one-line task list and succinct advanced feature mapping.
- 2026-01-14 11:14: Expanded TASKS.md with implementation task breakdown.
- 2026-01-14 11:26: Added schema hash helper in `agentic/schema`.
- 2026-01-14 11:30: Updated example to include agent-style message handling and refreshed Getting Started guide.
- 2026-01-14 11:33: Added events module and updated example to stream agent events.
- 2026-01-14 11:38: Added turn events, message IDs, and delta streaming to example.
