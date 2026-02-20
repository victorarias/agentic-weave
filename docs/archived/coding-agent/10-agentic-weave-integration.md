# Using agentic-weave as Core

## Goal
Reuse the existing agentic-weave library as the core engine for:
- TUI agent (local, low-memory)
- SaaS assistant agent (controller/orchestrator)
- Other bot-like agents

## Mapping: agentic-weave -> coding agent

- Agent loop
  - use: `agentic/loop`
  - responsibilities: tool loop, truncation, events, compaction hooks

- Compaction + budgets
  - use: `agentic/context` and `agentic/context/budget`
  - responsibilities: compaction policies, system prompt preservation

- Truncation
  - use: `agentic/truncate`
  - responsibilities: safe output truncation for tools/messages

- Tool scope/history
  - use: `agentic/toolscope` + `agentic/history`
  - responsibilities: tool call/result persistence + replay hooks

- Providers
  - use: `agentic/providers/*`
  - target: only Claude + Codex for MVP

- Events
  - use: `agentic/events`
  - responsibilities: stream model/tool updates to TUI + Remote

## Integration plan

### New components on top of agentic-weave
- `Supervisor` (queue + session orchestration)
- `JSONLStore` (local persistence)
- `TUI` (Go UI)
- `RemoteClient` (dial-out WS)
- `ConfigStore` (dynamic config)
- `Basic tools` (read/write)

### Composition strategy
- Build a thin wrapper around `loop.Runner` that accepts:
  - tool registry
  - compaction policy
  - event sinks
  - history store
- Use the same wrapper for:
  - TUI agent (local)
  - SaaS controller agent (remote orchestrator)
  - bot agents (Telegram, Slack, etc.)

### Reuse targets (explicit)
- The SaaS assistant agent MUST use the same `loop` + `tool` interfaces.
- The TUI agent MUST use the same core runner with different sinks.
- Bots (Telegram/web) should only adapt I/O and keep core logic identical.

## Reuse checklist
- [ ] Uses `loop.Runner` as the core execution engine.
- [ ] Uses the same `tool` registry and tool schemas.
- [ ] Uses the same compaction interface and policy plumbing.
- [ ] Emits events through `agentic/events` (or a compatible sink).
- [ ] Persists history through a `history.Store` adapter (JSONL or SaaS DB).
- [ ] Provider config normalized for Claude/Codex.
- [ ] No agent-specific branching in core logic (only at I/O boundaries).

## Risks / gaps to close
- JSONL persistence adapter for `agentic/history`.
- Tool streaming and truncation behavior alignment across local/remote.
- Provider config normalization (Claude + Codex only).
