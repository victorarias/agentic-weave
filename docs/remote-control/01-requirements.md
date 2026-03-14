# Remote Agent Control — Requirements

**Status (2026-03-13):** Active. Tier 0 and Tier 1 are now implemented with an RPC-backed wrapper around `pi --mode rpc`, a relay server, and local/relay inspector paths. Daemon-managed spawn/load, interactive PTY attach/takeover, and ACP shim work remain future tiers.

Gathered 2026-02-20 via structured interview.

## Naming

- **weave** — the monorepo and project name
- **weave-core** — the existing agentic loop library (`core/`)
- **weave-*** — all remote control tooling: `weave-relay`, `weave-wrapper`, `weave-daemon`, `weave-inspect`, `pi-bridge`, `weave-protocol`, `weave-fakepi`

## Monorepo Layout

```
weave/
├── go.work                          # Go workspace
├── core/                            # weave-core (existing agentic loop lib)
│   └── go.mod                       #   github.com/victorarias/weave/core
├── weave-protocol/                     # shared Go types (orchestrator imports this)
│   └── go.mod                       #   github.com/victorarias/weave/weave-protocol
├── weave-relay/                        # relay server
│   └── go.mod
├── weave-wrapper/                      # PTY wrapper (one per agent)
│   └── go.mod
├── weave-daemon/                       # host daemon (one per machine)
│   └── go.mod
├── weave-inspect/                      # CLI inspector / dev tools
│   └── go.mod
├── weave-fakepi/                   # test harness (fake pi-mono)
│   └── go.mod
├── extensions/
│   └── pi-bridge/                   # pi-mono extension (TypeScript)
│       ├── package.json
│       └── src/
├── docs/
├── examples/
└── tests/                           # cross-component integration tests
```

## Components

| Component | Dir | Language | Role |
|-----------|-----|----------|------|
| **Orchestrator** | *(external repo)* | Go | Assigns tasks, steers agents. Imports `weave-protocol`. We spec its interface only. |
| **weave-relay** | `weave-relay/` | Go | WebSocket multiplexer, session registry, spawn coordination, recovery. |
| **weave-wrapper** | `weave-wrapper/` | Go | PTY proxy around pi-mono. Connects to relay. Multiplexes structured + raw I/O. |
| **pi-bridge** | `extensions/pi-bridge/` | TypeScript | Pi-mono extension. Message injection, event streaming, tool interception. |
| **weave-daemon** | `weave-daemon/` | Go | Host daemon. Listens for spawn commands, launches wrapper+pi-mono pairs. |
| **weave-protocol** | `weave-protocol/` | Go | Shared types (messages, enums, state machine). Imported by orchestrator. |
| **weave-inspect** | `weave-inspect/` | Go | CLI inspector for debugging and observability. |
| **weave-fakepi** | `weave-fakepi/` | Go | Test harness. Fake pi runtime that can speak pi RPC (and later the bridge protocol if needed). |

## Core Requirements

### R1 — Agent-to-Agent Control (No Human in Loop)
- Orchestrator steers agents via **conversational back-and-forth** (not fire-and-forget).
- Any agent can spawn and control **child agents** (hierarchical subagent model).
- Control flows through the relay; agents never connect directly to each other.

### R2 — Human Attach on Demand
- Single attach per session (one controller at a time).
- Three modes, **switchable at runtime**:
  1. **Observe** — read-only stream of events.
  2. **Inject** — send messages into the agent's conversation.
  3. **Takeover** — full terminal control of the pi-mono TUI.
- Two attach paths:
  - **Remote**: web UI connecting through the relay.
  - **Local**: SSH into the machine, wrapper forwards the real PTY.

### R3 — Live Session Inspection
- Full event log: messages, tool calls, file diffs, metrics, state transitions.
- Filterable by type, severity, time range.
- Ephemeral buffer in relay for late-joining observers.

### R4 — Dynamic Spawn
- Orchestrator sends spawn commands to the relay.
- Relay routes to the appropriate host daemon.
- Host daemon launches a wrapper+pi-mono pair.
- Dynamic machine pool — hosts come and go.

## Core Design Principles

These principles should constrain implementation, especially early prototypes.

1. **Push, not scrape.**
   - Prefer structured events from the bridge/runtime.
   - Do not make prompt-prefix parsing, PTY scraping, or process-name heuristics
     the primary source of truth for readiness, busy/idle state, or tool activity.
2. **Progressive integration tiers.**
   - The architecture should support a walking skeleton that exercises the end-to-end
     loop quickly, then deepens integration incrementally.
3. **Runtime-specific details belong in adapters/presets.**
   - Avoid scattering pi-specific assumptions across relay, orchestrator, and state machine code.
4. **Delivery semantics must be explicit.**
   - The system should distinguish between queued, idle-boundary, and interrupting delivery.
5. **Hooks/plugins are integration helpers, not the control plane.**
   - They may enrich lifecycle and permissions, but must not replace the relay protocol.

## ACP Alignment Stance

We should treat **Agent Client Protocol (ACP)** as the reference model for the
**client ↔ agent session boundary**, but **not** as a replacement for the remote
control plane itself.

What ACP should shape:
- lifecycle negotiation (`initialize` + protocol version)
- capability negotiation (prompting, terminal, permissions, resume, streaming)
- first-class session creation vs session load/resume
- prompt/cancel semantics
- a normalized streamed `session.update` surface
- explicit permission requests / responses

What remains weave-specific:
- host discovery and spawn routing
- relay session registry and attach locks
- observe / inject / takeover modes
- raw PTY forwarding and resize
- parent/child session hierarchy
- daemon/wrapper recovery policy

Design implication:
- the relay protocol should be able to expose an **ACP-aligned facade or adapter**
  later without changing the underlying relay/daemon/wrapper architecture
- remote-control-specific features should be expressed as capability-gated
  extensions rather than leaking into the baseline prompt-turn lifecycle

## Architectural Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Transport | WebSocket relay | NAT traversal, multi-attach path, central multiplexing. |
| Discovery | Custom lightweight registry (part of relay) | Purpose-built, no external dependencies. |
| Auth | Token-based | Simple, stateless. Sufficient for v1. |
| Persistence | Agent-side (pi-mono JSONL) + ephemeral relay buffer | Accept pi-mono coupling for v1. |
| Failure handling | Configurable per-task | Auto-restart, mark-failed, or checkpoint-retry. |
| Pi-mono coupling | Accept for v1 | Use pi's RPC format first; add the interactive extension bridge only when PTY attach/takeover is needed. |
| Agent runtime mode | Tiered | Tier 0 uses RPC mode for the walking skeleton; later tiers can add interactive PTY ownership. |
| Agent roles | Deferred | Generic agents for v1. Role system designed later. |

## Runtime Model

Tier 0 walking skeleton:

```
  Client ──JSON/Unix socket──► Wrapper ──JSONL stdin/stdout──► pi --mode rpc
```

Later interactive/attach tiers:

```
  Orchestrator ──JSON/WS──► Relay ──JSON/WS──► Wrapper ──local bridge──► pi-bridge extension
                                                  │                           │
                                                  │ PTY ◄──────────────────► pi interactive TUI
                                                  │
  Human (SSH/web) ◄──PTY/WS──────────────────────┘
```

- Tier 0 prefers **pi RPC mode** to prove the control loop quickly.
- Later attach/takeover tiers can run pi in **interactive mode** with wrapper-owned PTY.
- **Structured channel** always stays authoritative for orchestration semantics.
- **Raw PTY channel** is a later layer for human takeover, not the primary source of truth.
- When a human attaches in takeover mode, orchestrator input is paused.
