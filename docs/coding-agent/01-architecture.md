# Architecture

## Modules (Go packages)
- supervisor: input queue, session routing, agent lifecycle
- agent: LLM loop + tool dispatch
- eventbus: pub/sub for TUI + RemoteClient
- tui: rendering + local input
- remote: dial-out WebSocket client
- storage/jsonl: session persistence
- config: dynamic config + watchers
- tools: registry + built-ins (read/write)

## Core data flow
```
Local TUI input ─┐
Remote input ────┼─> Supervisor.Enqueue(sessionID, input)
                 └─> AgentLoop.Run(sessionID)
AgentLoop -> ToolRegistry -> ToolResult
AgentLoop -> EventBus -> (TUI + RemoteClient)
Storage <- events append (JSONL)
```

## Concurrency model
- Supervisor owns a per-session FIFO queue.
- AgentLoop is single-flight per session.
- EventBus is non-blocking (drops/backs pressure configurable).

## Memory guardrails
- JSONL append-only; replay on load.
- Streaming tool output; no giant buffers in UI.
- LRU for rendered view cache.
