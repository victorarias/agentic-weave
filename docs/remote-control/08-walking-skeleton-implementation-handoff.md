# Walking Skeleton Implementation Handoff

**Status:** Active handoff plan — Tier 0, Tier 1, and the first Tier 2 spawn/load skeleton are implemented, plus a small hardening pass for session listing/state visibility; Tier 3 delivery semantics and the richer confirm-style permission lifecycle are now in place, Tier 4 has a human observe/inject slice plus a small ergonomics pass (`watch`, explicit attachment status details, same-identity observe→inject escalation), and Tier 5 has started with takeover control semantics (queued orchestrator prompts + takeover-held permission authority) ahead of PTY byte forwarding. Reproducible real relay validation now exists via `scripts/remotecontrol-permission-smoke.sh`, `scripts/remotecontrol-attach-smoke.sh`, and `scripts/remotecontrol-takeover-queue-smoke.sh`
**Date:** 2026-03-10
**Audience:** another implementation agent
**Related:**
- `docs/remote-control/01-requirements.md`
- `docs/remote-control/03-control-protocol.md`
- `docs/remote-control/04-architecture-and-plan.md`
- `docs/remote-control/06-testing-strategy.md`
- `docs/remote-control/07-acp-shim.md`

---

## Why this document exists

The remote-control project is ambitious and has a high risk of turning into a
large pile of half-connected parts.

So this handoff is optimized for:
- **walking skeleton first**
- **real end-to-end slices early**
- **fast feedback from Victor**
- **minimal speculative scaffolding**
- **clear stopping points after each tier**

This is not a “complete all subsystems” plan.
It is a plan to get to a thin but real system as fast as possible, then extend
it while preserving the architecture.

---

## Non-negotiable implementation rules

1. **Push, not scrape**
   - Prefer structured bridge/runtime events over PTY parsing.
   - Do not make terminal scraping the primary truth for readiness or busy/idle.

2. **One thin vertical slice beats broad scaffolding**
   - Each tier must end with something a human can run and observe.

3. **Keep runtime-specific behavior localized**
   - If pi-specific logic spreads into relay/orchestrator/state-machine code,
     stop and introduce an adapter/preset abstraction.

4. **Do not build takeover early**
   - Structured session control must work before raw PTY control exists.

5. **Do not build ACP shim early**
   - First stabilize the native relay/session protocol.

6. **Stop after each tier and evaluate**
   - The user should be able to try it, react, and redirect.

---

## What success looks like

The first useful success is **not** “remote multi-host orchestration works.”

The first useful success is:
- a local wrapper can launch pi in RPC mode
- a simple client can send a prompt through the wrapper socket
- pi emits structured updates back
- cancel works
- the session stops cleanly

That is enough to validate the architecture.

---

## Recommended implementation order

Implement only the first **three tiers** before considering anything beyond them.
They are the real walking skeleton.

---

# Tier 0 — Local vertical slice

## Goal

Prove the core **client ↔ wrapper ↔ pi RPC** loop locally.
No relay. No daemon. No host routing. No attach. No ACP shim.

## Deliverable

A local command/demo where:
1. wrapper starts `pi --mode rpc`
2. wrapper exposes a Unix socket control surface
3. local client sends one prompt
4. pi emits structured `session.update`
5. client sees streaming output
6. client sends cancel
7. wrapper shuts down cleanly

## Scope

### Build
- a minimal wrapper process
- a minimal local control transport (Unix domain socket)
- a normalization layer from pi RPC events to our session protocol
- a minimal local dev client or CLI command

### Required protocol support
Only these commands/events are required in Tier 0:
- `initialize`
- `session.prompt`
- `session.cancel`
- `session.agent_ready`
- `session.update`
- one terminal shutdown path

### `session.update` kinds required in Tier 0
Implement only:
- `lifecycle`
- `message_delta`
- `message_complete`
- `error`
- `complete`

Do **not** implement tool events yet unless they come nearly for free.

## Suggested file/package targets

These are suggestions, not a hard law.

- `cmd/weave-wrapper/`
  - wrapper main
  - local socket server
  - pi process launch in RPC mode
- `remotecontrol/protocol/`
  - shared envelope types
  - `initialize`, `session.status`, `session.prompt`, `session.cancel`
  - `session.update` schema
- `remotecontrol/local/`
  - pi RPC client glue
  - event normalization
- optional local test/dev client:
  - `cmd/weave-inspect/` with a `local` mode

## Tier 0 smoke test

Human-run demo:
```bash
# terminal 1
weave-wrapper --socket /tmp/weave-demo-1.sock --session demo-1

# terminal 2
weave-inspect local --socket /tmp/weave-demo-1.sock init
weave-inspect local --socket /tmp/weave-demo-1.sock prompt "say hello and then summarize this repo"
weave-inspect local --socket /tmp/weave-demo-1.sock cancel
```

Expected:
- ready event appears
- message deltas stream
- message completion appears
- cancel is acknowledged
- process exits or returns to idle cleanly

## Tier 0 acceptance criteria

- pi launches from wrapper in RPC mode
- wrapper socket accepts a local client reliably
- a prompt is delivered without tmux/send-keys hacks
- structured updates flow back to client
- cancel works at least once in a real run
- failure is visible as structured error, not silent hang

## Tier 0 stop-and-evaluate questions

1. Is the RPC-backed wrapper seam stable enough to keep building on?
2. Does `session.update` feel expressive enough already?
3. Are we relying on hidden pi behavior we should isolate now?
4. Did we accidentally make PTY scraping necessary for core flow?

If answers are bad, fix Tier 0 before moving on.

---

# Tier 1 — Single-session relay skeleton

## Goal

Insert the relay into the path while keeping everything else intentionally small.

## Deliverable

A real end-to-end path:
- client → relay → wrapper → pi → relay → client

Still only one host/runtime shape. No dynamic host routing.

## Scope

### Build
- WebSocket relay
- auth handshake
- `initialize` negotiation
- one-session registration / routing
- per-session ordered event forwarding

### Keep intentionally out of scope
- host selection
- daemon-managed spawn
- human attach
- PTY forwarding
- session hierarchy

## Required commands/events
Support these through the relay:
- `auth`
- `initialize`
- `session.prompt`
- `session.cancel`
- `session.update`
- `session.agent_ready`

## Suggested implementation shape

- `weave-relay/`
  - auth
  - client registry
  - session routing table
  - ordered event fanout
- `weave-wrapper/`
  - relay client mode
  - session registration
- `weave-protocol/`
  - auth + ack + error envelopes
  - capability ack payload

## Tier 1 smoke test

Human-run demo:
```bash
# terminal 1
weave-relay --addr :8080 --token dev-token

# terminal 2
weave-wrapper --relay ws://localhost:8080 --token dev-token --session demo-1

# terminal 3
weave-inspect relay --relay ws://localhost:8080/ws --token dev-token --session demo-1 init
weave-inspect relay --relay ws://localhost:8080/ws --token dev-token --session demo-1 prompt "readme-level summary please"
```

Expected:
- wrapper authenticates and registers
- client authenticates and initializes
- prompt reaches pi
- `session.update` streams back through relay in order

## Tier 1 acceptance criteria

- `initialize` negotiation works through relay
- ordered `session.update` forwarding works
- one wrapper and one client can interact through the relay
- errors and disconnects are visible and not ambiguous

## Tier 1 stop-and-evaluate questions

1. Does the relay API still feel clean, or is it already leaking wrapper internals?
2. Is `initialize` negotiation enough, or do we need better capability scoping?
3. Does the client need raw fine-grained events yet, or can it live on `session.update`?

---

# Tier 2 — Spawn / load / identity skeleton

## Goal

Make **session identity** and **runtime replacement** real before building more user-facing features.

This is the tier where the architecture becomes trustworthy.

**Status note (2026-03-13):** a first working skeleton now exists: relay-managed local wrapper spawning, explicit session/runtime registry state, `session.status`, `session.spawn`, `runtime.stop`, `session.load`, and a real pi-backed resume smoke test.

## Deliverable

A demo where:
1. a session is spawned fresh
2. it gets a stable `session_id`
3. the runtime dies or is stopped
4. a new runtime loads the same persisted session
5. the client continues talking to the same logical session

## Scope

### Build
- `runtime_id` vs `session_id` split
- `session.spawn`
- `session.load`
- persisted session handle support
- minimal runtime preset / adapter registry

### Mandatory cleanup before feature growth
Do not treat Tier 2 as pure feature work.
Before or alongside spawn/load, clean up the Tier 0/1 skeleton so it does not
solidify into accidental architecture.

Required cleanup outcomes:
- wrapper internals are split more clearly between:
  - pi runtime process/RPC control
  - peer transport management (local socket vs relay)
  - command dispatch / event normalization
- relay code stays transport/control-plane focused, not runtime-specific
- `session_id` and `runtime_id` are explicit and hard to confuse in code paths
- the runtime adapter/preset seam exists early enough to stop more pi-specific branching

### Registry requirements
Even if only pi exists, add a tiny runtime descriptor now.
Suggested fields:
- `name`
- `command`
- `args`
- `bridge_strategy`
- `resume_strategy`
- supported capabilities
- delivery semantics supported
- permission support
- readiness strategy

This must be config/data-driven enough to prevent scattered pi-specific branches.

## Required commands/events
- `session.spawn`
- `session.load`
- stable `session_id`
- distinct `runtime_id`
- `session.update(kind=lifecycle|status|complete|error)`

## Tier 2 smoke test

Human-run demo:
```bash
# spawn a new session
weave-inspect relay --relay ws://localhost:8080/ws --token dev-token --session demo-2 spawn
weave-inspect relay --relay ws://localhost:8080/ws --token dev-token --session demo-2 prompt "say one sentence"

# stop runtime
weave-inspect relay --relay ws://localhost:8080/ws --token dev-token --session demo-2 kill-runtime

# load existing session into a new runtime
weave-inspect relay --relay ws://localhost:8080/ws --token dev-token --session demo-2 load
weave-inspect relay --relay ws://localhost:8080/ws --token dev-token --session demo-2 prompt "continue"
```

Expected:
- same logical session continues
- new runtime is visible as distinct runtime instance
- client does not need to understand pi session path details

## Tier 2 acceptance criteria

- session identity survives runtime replacement
- runtime identity is visible and separate
- `session.load` works end-to-end at least once
- resume path is real, not a stub
- runtime adapter/preset exists, even if minimal
- Tier 0/1 prototype seams have been cleaned up enough that adding Tier 3 will not force a large rewrite

## Tier 2 stop-and-evaluate questions

1. Are session and runtime semantics still clean?
2. Did we expose too much pi persistence detail into protocol/client space?
3. Is the preset/adapter abstraction pulling its weight yet?
4. Did we actually pay down the Tier 0/1 cleanup debt, or merely add features on top of it?

If not, tighten before continuing.

**Hard gate:** do not start Tier 3 until the Tier 2 cleanup/refactor work has landed. If spawn/load works but the wrapper/relay/runtime boundaries are still mushy, Tier 2 is not done.

---

# Tier 3 — Permission + delivery semantics

## Goal

Validate the two most important behavioral seams:
- explicit delivery policy
- explicit permission mediation

## Deliverable

A demo where:
- one prompt is queued while busy
- one prompt interrupts current work
- one tool request requires permission
- allow/deny changes behavior deterministically

## Scope

### Add delivery policies
At minimum:
- `queue`
- `deliver_when_idle`
- `interrupt`

Do not bury these inside ambiguous priority flags.

### Add permission flow
- runtime emits `permission_request`
- authoritative actor answers
- timeout resolves deterministically to deny
- result appears in normalized updates

## Required `session.update` kinds for this tier
Add:
- `tool_begin`
- `tool_end`
- `permission_request`
- `permission_resolved`
- `status`

## Tier 3 smoke test

Human-run demo:
```bash
weave-inspect prompt demo-3 --delivery deliver_when_idle "normal request"
weave-inspect prompt demo-3 --delivery interrupt "stop and summarize"
# runtime requests permission for a command
weave-inspect allow demo-3 perm-1
weave-inspect deny demo-3 perm-2
```

Expected:
- queued prompt waits
- interrupt prompt preempts
- permission responses unblock or block deterministically

## Tier 3 acceptance criteria

- delivery semantics are explicit in code and protocol
- permission lifecycle invariants are real, not just documented
- normalized updates are still clean after adding these richer behaviors

---

# What should explicitly wait until later

Do **not** implement these before Tier 3 is working:
- takeover
- raw PTY forwarding
- human attach UI
- multi-host daemon routing
- ACP shim
- child sessions / hierarchical spawn

These are important, but they should sit on top of a working structured session core.

---

# Suggested work split for another agent

A second agent should implement this as **three narrow PRs**, not one giant branch.

## PR 1 — Tier 0
- local wrapper
- RPC handshake + local socket protocol
- prompt/cancel
- normalized updates
- local smoke demo

## PR 2 — Tier 1
- relay auth/init/routing
- wrapper relay mode
- simple client through relay
- ordered update forwarding

## PR 3 — Tier 2
- spawn/load
- runtime/session identity split
- minimal runtime preset registry
- resume smoke demo

Only after those should PR 4 tackle Tier 3.

---

# Advice to the implementing agent

1. **Bias hard toward runnable demos.**
   - If a piece cannot be exercised manually, it is probably too abstract for this phase.

2. **Prefer one real runtime over fake generality.**
   - It is fine if V1 only truly supports pi, as long as the code shape leaves room for adapters.

3. **Don’t gold-plate the relay too early.**
   - Tier 1 relay should be embarrassingly simple.

4. **Keep `session.update` sacred.**
   - Do not turn it into a leak of random internal event names.

5. **Treat PTY support as a second system.**
   - Structured control first, terminal takeover second.

6. **Leave ACP shim for later.**
   - The native protocol must feel good before we wrap it.

---

# Handoff checklist

Before handing back to Victor after any tier, provide:
- exact commands to run the demo
- what is real vs stubbed
- what invariants are now proven
- what felt shaky during implementation
- whether the next tier should proceed unchanged or be revised

The goal is not to “finish remote agent control.”
The goal is to make the architecture real early enough that feedback can shape it.
