# V1 Architecture and Phased Plan

**Status (2026-03-14):** Tier 0, Tier 1, the first Tier 2 spawn/load identity skeleton, Tier 3 delivery/permission semantics, and an initial Tier 4 human observe/inject slice are complete and validated. Implemented pieces now include `cmd/weave-wrapper` (local + relay modes), `cmd/weave-inspect` (local + relay modes plus relay `sessions` / `spawn` / `load` / `kill-runtime` / `status` / `attach` / `detach` / one-shot `inject`, explicit prompt delivery flags, and `allow` / `deny` for permission responses), `cmd/weave-relay`, `remotecontrol/protocol`, `remotecontrol/local`, `remotecontrol/relay`, `remotecontrol/runtime`, and `remotecontrol/session`. A real relay-managed pi resume smoke test works end-to-end, the confirm-style permission lifecycle is hardened, and humans can now attach in `observe` or `inject` mode with relay-enforced single-controller semantics and inject-mode permission authority. Real validation uses committed smoke scripts for both permission and observe/inject flows.

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                        ORCHESTRATOR (existing Go service)            │
│                                                                      │
│  Implements: WebSocket client to relay                               │
│  Uses: session.spawn, agent.message, session.attach, registry.*     │
│  Receives: all events for sessions it controls                       │
└────────────────────────┬─────────────────────────────────────────────┘
                         │ WebSocket (JSON)
                         ▼
┌──────────────────────────────────────────────────────────────────────┐
│                     WEAVE-RELAY (Go)                        │
│                                                                      │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────┐  ┌──────────────┐ │
│  │  Session     │  │  Registry    │  │  Auth    │  │  Event       │ │
│  │  Manager     │  │  (hosts +    │  │  (token  │  │  Buffer      │ │
│  │  (state      │  │   sessions)  │  │   check) │  │  (ring buf   │ │
│  │   machine)   │  │              │  │          │  │   per session)│ │
│  └─────────────┘  └──────────────┘  └──────────┘  └──────────────┘ │
│                                                                      │
│  ┌──────────────────┐  ┌────────────────┐                           │
│  │  Spawn Router    │  │  Attach Lock   │                           │
│  │  (host selection │  │  Manager       │                           │
│  │   + forwarding)  │  │  (single-ctrl) │                           │
│  └──────────────────┘  └────────────────┘                           │
└──────────┬────────────────────────────────────┬──────────────────────┘
           │ WebSocket                          │ WebSocket
           ▼                                    ▼
┌──────────────────────┐             ┌─────────────────────────┐
│ WEAVE-DAEMON (Go)  │             │  HUMAN CLIENT           │
│  (one per machine)   │             │  (web UI or CLI)        │
│                      │             │                         │
│  Receives: spawn,    │             │  Sends: attach, detach, │
│    kill commands      │             │    escalate, messages,  │
│  Reports: host_joined│             │    pty.input             │
│    host_left         │             │  Receives: events,      │
│  Manages: wrapper    │             │    pty.output            │
│    processes         │             └─────────────────────────┘
└──────────┬───────────┘
           │ (spawns)
           ▼
┌──────────────────────────────────────────────┐
│  WEAVE-WRAPPER (Go, one per agent)              │
│                                              │
│  ┌────────────┐  ┌───────────────────┐       │
│  │ PTY Master │  │ Relay WS Client   │       │
│  │ (owns the  │  │ (structured       │       │
│  │  virtual   │  │  channel)         │       │
│  │  terminal) │  │                   │       │
│  └─────┬──────┘  └────────┬──────────┘       │
│        │                  │                  │
│        │    bridge (local socket / pipe)     │
│        │                  │                  │
│  ┌─────▼──────────────────▼──────────────┐   │
│  │         PI-MONO (interactive mode)    │   │
│  │                                       │   │
│  │  ┌─────────────────────────────────┐  │   │
│  │  │  pi-bridge extension (TS)    │  │   │
│  │  │                                 │  │   │
│  │  │  - Receives commands from       │  │   │
│  │  │    wrapper via bridge           │  │   │
│  │  │  - Injects messages into        │  │   │
│  │  │    pi-mono conversation         │  │   │
│  │  │  - Streams events back to       │  │   │
│  │  │    wrapper                      │  │   │
│  │  │  - Can intercept tool calls     │  │   │
│  │  └─────────────────────────────────┘  │   │
│  └───────────────────────────────────────┘   │
└──────────────────────────────────────────────┘
```

## ACP Compatibility Stance

ACP is the right inspiration layer for **how a client talks to a running coding
agent**, but it is not enough for our full remote-control problem.

Therefore V1 should:
- keep the custom relay / daemon / wrapper / PTY architecture
- adopt ACP-like session semantics where they fit:
  - `initialize`
  - `session.new`
  - `session.load`
  - `session.prompt`
  - `session.update`
  - `session.cancel`
  - permission request / response
- keep spawn, host routing, attach/takeover, and parent/child session control as
  weave-specific capability-gated extensions

This gives us:
- cleaner orchestrator semantics
- a more standard editor/client integration surface later
- less protocol churn if we eventually add an ACP shim

## Session vs Runtime Semantics

The implementation should preserve this distinction:
- **runtime** = one live wrapper + pi-mono process tree
- **session** = the durable conversation identity that may survive runtime restart
- **attach state** = who currently controls or observes that runtime/session

Operational rules:
- `session.spawn` creates a new runtime and normally a fresh session
- `session.load` creates a new runtime attached to an existing persisted session
- failure recovery may replace the runtime while preserving session identity
- attach / takeover never creates a new session; it only changes who is allowed
  to interact with the current runtime

If implementation pressure forces these concepts to blur, that should be treated
as a design problem rather than solved ad hoc in the relay.

## Runtime Preset / Adapter Registry

Even if V1 only targets pi-mono, the architecture should introduce a small
runtime descriptor model early rather than hardcoding runtime-specific behavior
throughout the stack.

Suggested fields:
- runtime name / id
- launch command + args
- bridge strategy
- resume strategy
- supported capabilities
- prompt delivery semantics supported (`queue`, `deliver_when_idle`, `interrupt`)
- permission mediation support
- readiness strategy (structured ready event preferred; heuristic fallback only as last resort)
- hook/plugin install details

Why:
- keeps pi-specific behavior localized
- makes future runtimes possible without protocol redesign
- prevents relay/orchestrator code from turning into provider-specific switch statements

## Walking Skeleton Strategy

This project is risky if we optimize for completeness before we optimize for an
end-to-end vertical slice.

So the implementation strategy should be:
- connect multiple pieces as early as possible
- prefer narrow, ugly-but-real flows over broad partial scaffolding
- validate real seams early (bridge, session identity, update stream, cancel path)
- deepen the integration in layers

A good early slice is one where:
- a runtime starts
- a client sends one prompt
- the runtime emits structured updates
- cancel works
- the session can stop cleanly

Even if nothing else exists yet, that slice exercises the core architecture.

## Tier 0 Runtime Integration Strategy

For the first walking skeleton, prefer **pi RPC mode over stdin/stdout** instead of
starting with the interactive PTY + extension bridge path.

Why:
- RPC mode already gives us a structured process-integration API.
- It proves the control protocol, event normalization, and cancel path faster.
- It avoids building a bridge and PTY supervision before we know the basic
  session model feels right.

That means Tier 0 should look like:
- local client ↔ Unix socket ↔ Go wrapper ↔ `pi --mode rpc`

The **interactive PTY + `pi-bridge` extension** path still matters for later
attach/takeover tiers, but it should come after the RPC-backed control loop is
real and stable.

For later tiers that do need an in-process bridge, the preferred local transport
remains a Unix domain socket.

## Phased Implementation Plan

These phases should be treated as **integration tiers**, not calendar promises.
Each tier should end with a real demoable slice, even if it is ugly and only
partially useful.

### Tier 0 — Local Vertical Slice

**Goal**: prove the core client ↔ runtime loop locally before introducing relay or hosts.

Deliverables:
1. **Go wrapper** that:
   - spawns `pi --mode rpc`
   - exposes a Unix domain socket for local control
   - accepts `initialize`, `session.prompt`, and `session.cancel`
   - normalizes pi RPC events into `session.agent_ready` and `session.update`
2. **Tiny local dev client / inspector** that:
   - connects to the wrapper socket
   - initializes capabilities
   - sends one prompt
   - prints streaming updates
3. **Smoke test**:
   - start wrapper
   - send one prompt
   - observe message delta + completion
   - cancel a second prompt
   - stop cleanly

**Rule:** do not add relay, PTY takeover, or extension/bridge work until this RPC-backed end-to-end slice is real.

### Tier 1 — Single-Session Relay Skeleton

**Goal**: put the relay in the middle without adding host-routing complexity yet.

Deliverables:
1. **Go relay server** with:
   - WebSocket endpoint
   - auth handshake
   - explicit `initialize` negotiation after auth
   - one-session event fanout
   - `session.update` forwarding with preserved ordering
2. **Wrapper updated** to:
   - connect to relay
   - register the session
   - forward updates through relay instead of only local stdout
3. **Simple orchestrator/dev client** that:
   - connects to relay
   - sends `initialize`
   - sends `session.prompt`
   - receives normalized `session.update`

**Smoke test:** orchestrator → relay → wrapper → pi → relay → orchestrator.

### Tier 2 — Spawn / Load / Identity Skeleton

**Goal**: make session identity and recovery real before adding more features.

**Status note (2026-03-13):** the current skeleton now includes a relay-managed local spawn/load path, explicit `session.status`, and a real `spawn → prompt → runtime.stop → load → prompt` smoke test against pi session persistence.

**Mandatory cleanup at the start of Tier 2:** before adding spawn/load behavior,
refactor the Tier 0/1 prototype so responsibilities are cleaner.
At minimum:
- separate wrapper runtime control from peer-transport management
- keep relay routing/session registry logic out of pi-specific runtime code
- make `session_id` vs `runtime_id` explicit in data structures and message flow
- introduce a small runtime adapter/preset seam so pi-specific behavior does not keep spreading

This cleanup is not optional bookkeeping. Tier 2 is the point where the current
walking-skeleton code stops being "just a prototype" and becomes the real base
for later features.

Deliverables:
1. Add explicit support for:
   - `session.spawn`
   - `session.load`
   - stable `session_id`
   - distinct `runtime_id`
2. Add a minimal **runtime preset/adapter registry** so runtime-specific behavior
   is configured, not scattered in control-plane code
3. Support one persisted-session resume path end-to-end
4. Land the cleanup/refactor above as part of the tier, not as follow-up debt

**Smoke test:** spawn fresh session, stop runtime, load same session into a new runtime.

**Gate:** do not begin Tier 3 until this cleanup has landed and the Tier 2 identity/runtime seams are clean enough that further features do not increase architectural debt.

### Tier 3 — Permission + Delivery Semantics

**Goal**: validate the hardest behavioral seams before human attach.

Deliverables:
1. Permission roundtrip:
   - runtime emits permission request
   - orchestrator responds allow/deny
   - runtime continues or blocks deterministically
2. Explicit prompt delivery semantics:
   - `queue`
   - `deliver_when_idle`
   - `interrupt`
3. Clear mapping from delivery policy to pi RPC / runtime behavior

**Smoke test:** send normal prompt while busy, then interrupting prompt, then a permission-gated tool call.

### Tier 4 — Human Observe / Inject

**Goal**: add human presence without full takeover yet.

Deliverables:
1. Attach lock manager in relay
2. Observe mode
3. Inject mode
4. authority rules for permissions when human is attached

**Smoke test:** orchestrator controls session, human observes, human injects, orchestrator sees resulting updates.

### Tier 5 — Takeover + PTY Path

**Goal**: add raw terminal control only after structured control is already stable.

Deliverables:
1. PTY output forwarding to attached human
2. `pty.input`
3. `pty.resize`
4. takeover / de-escalation flow
5. queued orchestrator behavior during takeover

**Smoke test:** human escalates to takeover, drives the TUI, de-escalates, orchestrator resumes.

### Tier 6 — Remote Hosts + Failure Recovery

**Goal**: distribute runtimes across machines only after local semantics are working.

Deliverables:
1. Go host daemon that registers with relay
2. relay host routing
3. remote spawn/load
4. failure policy execution and runtime replacement under stable `session_id`

**Smoke test:** orchestrator spawns on remote host, wrapper dies, runtime is replaced, session survives.

### Tier 7 — ACP Shim + Hierarchical Sessions

**Goal**: expose a standard-ish external surface and then deepen orchestration.

Deliverables:
1. minimal ACP-compatible shim
2. optional `_weave/*` extensions for advanced features
3. parent/child session hierarchy support
4. child status / cascading kill policy

**Smoke test:** ACP client drives a weave-backed session; native client inspects the same session; optional child spawn works.

## What the Orchestrator Needs to Implement

The existing Go service needs a new module that:

1. **Connects** to the relay via WebSocket.
2. **Authenticates** with a token (`auth` command).
3. **Initializes** protocol version + client capabilities (`initialize`).
4. **Discovers** available hosts (`registry.list_hosts`).
5. **Spawns** fresh agents on hosts (`session.spawn`) or **loads** existing ones (`session.load`) with:
   - System prompt / task description
   - Failure policy
   - Extension configuration
6. **Steers** agents conversationally (`agent.message` / `session.prompt`) — reads responses, decides next message.
7. **Monitors** sessions via event stream, preferring normalized `session.update` for generic clients and fine-grained events for rich tooling.
8. **Handles** failures (receives `session.state_changed` to FAILED, decides whether to retry).
9. **Responds** to permission requests when human/policy approval is required.
10. **Manages** attach requests from humans (optional: orchestrator can observe who's attached).
11. **Kills** sessions when tasks complete (`session.kill`).

The orchestrator does NOT manage PTYs, host daemons, or pi-mono internals. It only speaks the control protocol.
