# Risks, Tradeoffs, and Open Questions

## Risks

### R1 — Pi-mono Coupling (HIGH)
**Risk**: We depend on pi-mono's extension API, interactive mode behavior, and message injection semantics. Upstream breaking changes could require significant rework.

**Mitigation**: The wv-bridge extension is the only coupling point. If pi-mono breaks, we rewrite one TS file — the wrapper, relay, and orchestrator are decoupled. V1 accepts this coupling explicitly.

**Signal to watch**: pi-mono's extension API stability. If it churns fast, consider abstracting earlier.

### R2 — PTY Multiplexing Complexity (MEDIUM → LOW)
**Risk**: The wrapper must correctly handle PTY semantics (terminal size, escape sequences, raw mode) for human attach. Incorrect handling causes garbled output or broken input.

**Mitigation**: Well-understood problem with established patterns. Use `creack/pty` (Go standard). Key architecture: single reader goroutine + ring buffer + subscriber fan-out. Escape sequence splitting is a non-issue for raw byte forwarding. Our single-attach model eliminates the hardest part (multi-writer contention). Reference implementations: GoTTY, Upterm, Coop. Estimated 8-12 working days.

**Critical implementation note**: Do NOT use `io.Copy` on PTY file descriptors — blocked reads cannot be cancelled (Go #58628). Use explicit read loops.

### R3 — Relay as Single Point of Failure (MEDIUM)
**Risk**: The relay is centralized. If it goes down, all control is lost. Agents keep running (they're independent processes), but you can't steer or observe them.

**Mitigation**:
- V1: accept this. The relay is a simple stateless-ish process — restart recovers quickly.
- Wrappers reconnect automatically on relay restart.
- Event buffer is lost on relay crash (agent-side JSONL remains).
- V2: consider relay HA (active-passive or multi-relay with shared state).

### R4 — Extension ↔ Wrapper Bridge Reliability (MEDIUM)
**Risk**: The Unix domain socket bridge between the Go wrapper and TS extension could drop, stall, or deadlock. The extension runs inside pi-mono's process — if pi-mono is busy (long tool execution), the extension's event loop may block.

**Mitigation**:
- Use async I/O on both sides (goroutines in Go, async/await in TS).
- Heartbeat on the bridge — wrapper detects extension silence → marks session FAILED.
- Keep bridge messages small and frequent (stream events, don't batch).

### R5 — Security in V1 (LOW but real)
**Risk**: Token-based auth without TLS means tokens can be sniffed on the network. No authorization model — any valid token can do anything.

**Mitigation**: V1 runs on trusted networks (internal VMs). Add TLS in V2. Add role-based authorization (orchestrator vs human vs daemon) in V2.

### R6 — Agent History Loss on Host Failure (LOW)
**Risk**: If a host machine dies, all JSONL history for agents on that host is lost.

**Mitigation**: Accepted for V1. Options for V2:
- Host daemon periodically syncs JSONL to shared storage.
- Relay persists a copy of all events.
- NFS/shared filesystem for agent working directories.

---

## Tradeoffs

### T1 — Interactive Mode (chosen) vs RPC Mode
**Chose**: Always interactive mode with PTY wrapper.
**Gave up**: Simpler RPC pipe. RPC mode has a clean JSON protocol but no human-attach PTY.
**Why**: Human takeover of the real pi-mono TUI is a core requirement. Interactive mode is the only way to get that.

### T2 — Custom Relay (chosen) vs Off-the-shelf Message Broker
**Chose**: Purpose-built Go relay.
**Gave up**: Battle-tested infrastructure (NATS, RabbitMQ, Redis pub/sub).
**Why**: The relay needs domain-specific logic (session state machine, attach locks, spawn routing). A generic broker would still need a custom service on top. Fewer moving parts.

### T3 — Accept Pi-mono Coupling (chosen) vs Abstract from Day 1
**Chose**: Use pi-mono's formats directly in V1.
**Gave up**: Clean abstraction boundary from the start.
**Why**: We don't yet know which events matter most. Abstracting prematurely risks building the wrong abstraction. The coupling is contained in one extension file.

### T4 — Single Attach (chosen) vs Multi-Attach
**Chose**: Only one human controller at a time.
**Gave up**: Multiple humans collaborating on one session.
**Why**: Multi-attach introduces complex conflict resolution (whose keystrokes win in takeover? whose messages take priority?). Single-attach is safe and simple. Revisit if real need emerges.

### T5 — Unix Domain Socket Bridge (chosen) vs Other IPC
**Chose**: UDS for wrapper ↔ extension communication.
**Gave up**: Simpler pipe-based approach, or HTTP localhost.
**Why**: Bidirectional, fast, no port allocation. Clean for the "one wrapper per agent" model.

---

## Open Questions

### Q1 — Extension Message Injection Fidelity — RESOLVED
**Question**: When the extension injects a message into pi-mono's conversation, does pi-mono treat it identically to a real user message? Are there edge cases (mid-stream injection, injection during tool execution)?

**Answer**: Yes. `pi.sendUserMessage(content, options)` injects messages identically to real input. Mid-stream injection is explicitly supported with two modes:
- `deliverAs: "steer"` — interrupts after current tool finishes, remaining tool calls skipped
- `deliverAs: "followUp"` — waits for agent to complete all tools before delivering
- If agent is idle, message is sent immediately

Messages are marked `source: "extension"` in the `InputEvent` (distinguishable if needed) but otherwise participate in the normal conversation flow, get stored in session history, and trigger agent loops.

**Ref**: `packages/coding-agent/src/core/extensions/types.ts:1013-1016`, `examples/extensions/send-user-message.ts:33-74`

**Design implication**: The wrapper can use `steer` for high-priority orchestrator interrupts and `followUp` for normal conversational steering. Maps cleanly to our `agent.message` command's `priority` field.

### Q2 — PTY Size Negotiation — RESOLVED
**Question**: When a human attaches to a session, the human's terminal size may differ from the wrapper's virtual PTY size. How do we handle terminal resize?

**Answer**: Wrapper resizes the PTY to match the human's terminal on attach. `pty.Setsize()` (from `creack/pty`) triggers a kernel SIGWINCH to the child process, causing pi-mono's TUI to redraw at the new size. This is the standard pattern used by GoTTY, Upterm, and similar tools.

When nobody is attached, wrapper uses a sensible default (e.g. 120x40). On attach, resize to human's terminal. On detach, optionally resize back to default.

For WebSocket clients: resize messages sent as JSON (`{"type":"resize","rows":24,"cols":80}`). For SSH: resize comes via the SSH channel's `window-change` request.

### Q3 — Pi-mono Extension Event Coverage — RESOLVED
**Question**: Can the extension capture ALL events we need (assistant messages, tool calls, tool results, errors, token usage)?

**Answer**: Yes — comprehensive. 30+ event types covering everything we need:

| Our event | Pi-mono hook | Notes |
|-----------|-------------|-------|
| `agent.assistant_message` | `message_start` / `message_update` / `message_end` | Token-by-token streaming available |
| `agent.tool_call` (started) | `tool_execution_start` | Tool name, args, call ID |
| `agent.tool_call` (progress) | `tool_execution_update` | Streaming tool output |
| `agent.tool_call` (completed) | `tool_execution_end` | Result + duration |
| `agent.tool_permission` | `tool_call` event | Can return `{ block: true }` to deny |
| `agent.error` | `turn_end` / error states | Recoverable vs fatal |
| `agent.metrics` | `ctx.getContextUsage()` | Token usage per turn |
| Session lifecycle | `session_start`, `session_shutdown`, etc. | Full lifecycle |

Additionally: `tool_result` event allows **modifying** tool results after execution (useful for filtering/redacting sensitive output).

**Ref**: `packages/coding-agent/src/core/extensions/types.ts:798-817`, `runner.ts:570-628`

**Design implication**: We can map pi-mono events 1:1 to our protocol events. No lossy translation needed. The extension will be a straightforward event relay.

### Q4 — Hierarchical Spawn Auth
**Question**: When an agent spawns a subagent, what token does the child use? Does it inherit the parent's token? Should children have scoped tokens (less privilege)?

**Defer to**: Phase 6 design.

### Q5 — Conversation History on Resume — RESOLVED
**Question**: When an agent crashes and restarts (failure policy = restart), how do we feed it the conversation history?

**Answer**: Pi-mono natively supports session resume. Sessions are JSONL files at `~/.pi/agent/sessions/`. The RPC protocol has:
```json
{"type": "switch_session", "sessionPath": "/path/to/session.jsonl"}
```
Sessions are append-only — a crash leaves a valid (possibly truncated) JSONL file. On restart, pi-mono loads the full tree-structured history. Sessions have UUIDs and support forking (`parentSession` field).

**Ref**: `packages/coding-agent/docs/rpc.md:541-557`, `src/core/session-manager.ts:27-145`

**Design implication**: Recovery is simple — the host daemon restarts wrapper+pi-mono and tells it to `switch_session` to the existing JSONL file. No replay through the extension needed. The JSONL file IS the checkpoint.

**Session resume in interactive mode** (see Q11): Fully supported via `--session <path>` CLI flag or `ctx.switchSession()` from extensions.

### Q5b — Tool Interception and Delegation — NEW (from research)
**Finding**: Extensions can:
- **Block** tool calls before execution (`tool_call` event → `{ block: true }`)
- **Modify** tool results after execution (`tool_result` event → return patched result)
- **Fully delegate** tool execution by registering replacement tools with custom `Operations` backends

The SSH extension (`examples/extensions/ssh.ts`) demonstrates full delegation: it registers replacement `read`, `write`, `edit`, `bash`, etc. tools that proxy to a remote host. Operations interfaces exist for: `ReadOperations`, `WriteOperations`, `EditOperations`, `BashOperations`, `LsOperations`, `GrepOperations`, `FindOperations`.

**Ref**: `runner.ts:607-628`, `ssh.ts:114-150`, `docs/extensions.md:1340-1383`

**Design implication**: This means we could also build the inverse of our architecture — an agent running locally whose tools execute on a remote machine. Not needed for v1, but good to know the extension API supports it natively.

### Q6 — Relay Event Buffer Size
**Question**: How large should the per-session ring buffer be?

**Tentative**: 1000 events per session, configurable. Profile in Phase 2.

### Q7 — Web UI Technology
**Question**: What framework for the web UI in Phase 5?

**Defer to**: Phase 5 design.

### Q8 — Wrapper Binary Distribution
**Question**: How do wrapper + host daemon binaries get deployed to new machines?

**Defer to**: Phase 3 design. For V1, manual deployment is fine.

### Q9 — Agent Working Directory Isolation
**Question**: When multiple agents run on the same host, do they share a working directory or get isolated ones?

**Tentative**: Each agent gets its own working directory (git worktree or separate clone). Orchestrator manages coordination.

### Q10 — Rate Limiting and Backpressure
**Question**: If the orchestrator sends messages faster than an agent can process them, what happens?

**Tentative**: Wrapper queues up to N messages (e.g. 10). Beyond that, relay returns `RATE_LIMITED`. Orchestrator must wait for events before sending more. Pi-mono's `steer` vs `followUp` delivery semantics give us natural backpressure — `followUp` queues until the agent is idle.

### Q11 — Interactive Mode Session Resume — RESOLVED
**Question**: We chose interactive mode (not RPC) for PTY access. But `switch_session` is an RPC command. How does pi-mono resume a session in interactive mode?

**Answer**: Pi-mono's CLI fully supports session resume in interactive mode:
- `--session <path>` — open a specific session file directly
- `--continue` / `-c` — continue the most recent session
- `--resume` / `-r` — TUI picker to select a session

From the extension side, `ctx.switchSession(sessionPath)` works programmatically (returns `Promise<{ cancelled: boolean }>`), triggers `session_before_switch` / `session_switch` events, reloads all messages, and restores model + thinking level.

**Ref**: `packages/coding-agent/src/cli/args.ts:73-92`, `src/core/extensions/types.ts:313-314`

**Design implication**: Crash recovery in interactive mode is simple: host daemon restarts wrapper, wrapper spawns `pi --session /path/to/existing-session.jsonl`, pi-mono loads the full history and continues. No extension-level replay needed.

### Q12 — Interactive vs RPC — RESOLVED (staying with interactive)
**Decision**: Keep interactive mode. User wants native pi-mono TUI when SSH'd into the machine, with remote control from orchestrator/web when away.

**PTY multiplexing feasibility**: Confirmed feasible. ~8-12 working days to production quality.
- Use `creack/pty` (Go, de facto standard)
- Core pattern: single reader goroutine + ring buffer + subscriber fan-out (NOT `io.Copy` — leaks goroutines on PTY fds, Go issue #58628)
- Terminal resize: `pty.Setsize()` on attach, kernel delivers SIGWINCH, child redraws
- Single-attach model eliminates multi-writer contention — significantly simpler
- Reference projects: GoTTY (PTY→WebSocket), Upterm (terminal sharing), **Coop** (almost exactly our use case — agent terminal sidecar with dual structured + raw PTY channels)

**Key pitfalls to avoid**:
1. Never use `io.Copy` on PTY fds — use explicit read loop with ring buffer
2. Don't let slow WebSocket clients block the PTY reader (buffered channels + drop policy)
3. Ring buffer for catch-up on human attach (Coop uses 1 MiB circular buffer with offset-based replay)
4. One reader goroutine for PTY lifetime — subscribers come and go, reader never restarts

### Q13 — ACP Compatibility Stance — RESOLVED
**Question**: Should we replace our relay protocol with Agent Client Protocol (ACP), or only learn from it?

**Answer**: Learn from it, but do not replace the control plane with it.

ACP is a strong fit for the **client ↔ running agent** boundary:
- `initialize`
- `session.new`
- `session.load`
- `session.prompt`
- `session.update`
- `session.cancel`
- permission mediation

ACP is **not** sufficient for the full remote-control problem we are solving:
- host discovery and spawn routing
- daemon-managed process launch
- human attach / inject / takeover
- raw PTY forwarding + resize
- parent/child session hierarchy
- attach locks and failure policies

**Design implication**: Keep the relay / daemon / wrapper architecture, but make the session-facing API intentionally ACP-aligned so we can add an ACP adapter later without redesigning the runtime.
