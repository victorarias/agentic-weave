# ACP-Compatible Shim (MVP)

**Status:** Proposed
**Date:** 2026-03-10
**Related:**
- `docs/remote-control/01-requirements.md`
- `docs/remote-control/03-control-protocol.md`
- `docs/remote-control/04-architecture-and-plan.md`
- `docs/remote-control/05-risks-and-open-questions.md`

## Purpose

Define the smallest useful **Agent Client Protocol (ACP)** compatibility layer for
weave remote agent control.

The goal is **not** to replace the relay / daemon / wrapper architecture.
The goal is to expose a standard-ish **client ↔ running-agent** surface so ACP
clients can drive a weave session without knowing about:

- host daemons
- wrapper processes
- pi-mono internals
- PTY ownership
- relay session registry details

In other words:

> the ACP shim is an adapter over a weave session, not a new control plane.

---

## Why we want this

### 1. Interoperability

If editors or other clients adopt ACP, they can talk to weave without a
bespoke integration.

### 2. Cleaner boundary

Even if no third-party client uses it immediately, ACP gives us a disciplined
shape for:

- initialization
- capabilities
- prompt turns
- streaming updates
- cancellation
- permission handling
- session resume

### 3. Future-proofing without commitment

We can keep all weave-specific remote-control features while still offering a
more standard compatibility surface.

### 4. Safer protocol evolution

A shim forces us to separate:
- the generic session model
- the weave-specific orchestration features

That reduces the risk that the relay protocol becomes an unstructured grab-bag.

---

## Non-goals

The MVP ACP shim does **not** attempt to standardize or hide:

- host selection and spawn routing
- daemon management
- human attach modes
- takeover semantics
- raw PTY streaming
- parent/child session hierarchy
- registry/ops/admin features

Those remain weave-specific.

---

## Recommendation

Build the ACP shim as a **small adapter service** that talks to the relay as a
normal client.

### Why an adapter service instead of embedding in the relay?

Pros:
- easier to iterate or delete
- keeps relay simpler
- lets us validate usefulness before hardening it into core infrastructure
- creates a clear test seam

Cons:
- one extra process
- small amount of translation overhead

For MVP, the tradeoff is worth it.

---

## Proposed shape

```text
ACP Client
  ↕ JSON-RPC / ACP
wv-acp-shim
  ↕ weave remote-control protocol
wv-relay
  ↕
wv-daemon / wv-wrapper / wv-bridge / pi-mono
```

The shim owns:
- ACP method handling
- capability advertisement
- mapping ACP sessions to weave sessions
- update normalization
- permission request/response translation

The relay still owns:
- auth
- host/session registry
- spawn/load/kill
- event fanout
- attach locks
- PTY forwarding
- failure policy handling

---

## MVP ACP surface

The shim should support only the core session lifecycle.

### Required

1. `initialize`
2. `session/new`
3. `session/load`
4. `session/prompt`
5. `session/cancel`
6. `session/update`
7. permission request / response

### Explicitly unsupported in MVP

- terminal acquire/release via ACP
- takeover over ACP
- host selection via ACP core methods
- subagent hierarchy via ACP core methods
- registry / list-hosts / ops status via ACP core methods

Unsupported features should return a clear “not supported by ACP shim” error.

---

## Mapping: ACP ↔ weave

## 1. `initialize`

### ACP meaning
Client and agent negotiate protocol version and capabilities.

### Shim behavior
- accept ACP initialize
- establish a relay connection if not already established
- authenticate to relay with the shim's configured identity
- return shim-advertised capabilities based on:
  - relay capabilities
  - wrapper/runtime support
  - what the shim chooses to expose

### Notes
This is the right place to hide weave-specific complexity.
ACP clients should learn only what they can actually use through the shim.

---

## 2. `session/new`

### ACP meaning
Create a new agent session.

### Shim behavior
Translate to weave `session.spawn`.

Default behavior:
- select a default host using configured policy
- request `session.spawn` from relay
- store a mapping:
  - `acp_session_id`
  - `weave_session_id`
- subscribe to weave events for that session

### Important mismatch
ACP thinks in terms of “new session.”
weave thinks in terms of “spawn remote process on a host.”

The shim exists largely to bridge this mismatch.

### Host selection in MVP
Do **not** expose host selection in the ACP core surface.
Use one of:
- a default host policy in the shim
- optional config at shim startup
- a later custom ACP extension method

---

## 3. `session/load`

### ACP meaning
Load/resume an existing session.

### Shim behavior
Translate to weave `session.load` when a persisted session path or known resume
handle exists.

Possible supported forms:
- load by known weave session mapping
- load by persisted session path
- load by resume token owned by the shim

### Recommendation
For MVP, prefer one simple contract:
- the shim returns a `resume_handle` on `session/new`
- `session/load` accepts that handle
- the shim resolves it into weave session metadata

This avoids exposing pi session file paths directly to ACP clients.

---

## 4. `session/prompt`

### ACP meaning
Send a prompt turn to the agent.

### Shim behavior
Translate directly to weave `agent.message` / `session.prompt`.

Suggested mapping:
- ACP user input → `content`
- normal prompt → weave normal priority
- optional future ACP metadata → weave `priority=high` only if explicitly requested

### Rule
The shim should keep prompt-turn semantics simple.
Do not overload this path with attach/takeover concepts.

---

## 5. `session/cancel`

### ACP meaning
Cancel the current operation.

### Shim behavior
Translate directly to weave `agent.cancel` / `session.cancel`.

This is a very clean mapping and should be supported in MVP.

---

## 6. `session/update`

### ACP meaning
Client receives updates describing session progress.

### Shim behavior
The shim subscribes to weave events and emits a normalized ACP update stream.

### Source weave events
- `session.state_changed`
- `session.agent_ready`
- `agent.assistant_message`
- `agent.tool_call`
- `agent.tool_permission`
- `agent.error`
- `agent.metrics`

### Normalization rule
ACP clients should be able to function using `session/update` alone.

So the shim should collapse weave’s rich event stream into a smaller set of
update kinds, for example:
- `status`
- `message_delta`
- `message_complete`
- `tool_call`
- `tool_result`
- `permission_request`
- `error`
- `completed`

### Important design rule
Do **not** leak raw weave event names as the primary ACP surface.
If we want raw access later, expose it via a custom extension or debug mode.

---

## 7. Permission mediation

### ACP meaning
The agent asks the client for approval to perform some action.

### Shim behavior
Translate:
- weave `agent.tool_permission` → ACP permission request
- ACP permission response → weave `session.permission_response`

### Why this matters
This is one of the strongest reasons to support ACP compatibility.
Coding agents often need permission for:
- bash execution
- file writes
- destructive commands
- sensitive operations

ACP-style permission handling fits this very well.

---

## Session identity model

The shim should own the mapping between ACP and weave session IDs.

## Recommendation
Use distinct IDs:
- ACP session ID: public to ACP clients
- weave session ID: internal to adapter + relay

Why:
- cleaner separation
- easier future migration
- lets us load/bind existing weave sessions without exposing internal IDs

The shim should persist a small mapping store:

```text
acp_session_id -> weave_session_id, host_id, resume_handle, created_at
```

For MVP, this can be in-memory if we only care about live sessions.
If `session/load` matters across shim restarts, use a tiny durable store.

---

## Capability advertisement

The shim should advertise only what it truly supports.

### Suggested MVP capabilities
- prompt turns
- streaming updates
- cancel
- permission requests
- session load/resume

### Suggested “not yet” capabilities
- terminal management
- raw PTY access
- multi-controller attach
- takeover

If the ACP ecosystem expects explicit capability fields, the shim should mark
those unsupported rather than faking partial behavior.

---

## Error model

ACP clients should see ACP-shaped errors, not raw weave protocol errors.

### Translation examples
- weave `SESSION_NOT_FOUND` → ACP session-not-found error
- weave `INVALID_STATE` → ACP invalid-request / invalid-state error
- weave `HOST_UNAVAILABLE` during `session/new` → ACP unavailable error
- weave `ATTACH_CONFLICT` should not normally appear in MVP ACP surface because
  attach is not exposed

### Rule
The shim should preserve useful detail in structured metadata, but error names
and primary messages should stay ACP-oriented.

---

## Streaming model

The shim should support ACP streaming from the start.

### Why
Without streaming, the ACP surface becomes much less useful for coding agents.
Clients need to see:
- partial assistant output
- tool progress
- waiting-for-permission states
- completion/failure

### Internal behavior
- shim subscribes to relay session events
- shim maintains per-session update sequence
- shim emits normalized `session/update` notifications in order

### Nice-to-have later
- resumable update cursors
- replay after reconnect
- explicit gap notifications

These are good, but not required for MVP.

---

## Weave-specific extensions to reserve

Anything outside the MVP ACP core should be exposed through explicit custom
methods later rather than overloading the baseline.

Potential custom methods:
- `_weave/session/attach`
- `_weave/session/escalate`
- `_weave/session/takeover`
- `_weave/pty/input`
- `_weave/pty/resize`
- `_weave/registry/list_hosts`
- `_weave/session/spawn_on_host`
- `_weave/session/children`

This keeps the ACP-compatible surface clean while still allowing advanced weave
features.

---

## Security model for MVP

The shim authenticates to the relay using a configured service identity.
ACP clients authenticate to the shim using whatever mechanism the shim supports.

### MVP recommendation
- keep shim auth simple
- scope clients to only their own ACP sessions
- do not let ACP clients call raw weave registry/admin operations

The shim should behave like a restricted client-facing facade, not a relay admin
proxy.

---

## What success looks like

A generic ACP client can:
- connect and initialize
- create a session
- resume a session
- send prompts
- receive streaming updates
- cancel work
- approve/deny permissions

without needing to know anything about:
- wrappers
- daemons
- PTYs
- host routing
- pi-mono internals

That is enough to make the shim genuinely useful.

---

## What success does **not** require

The ACP shim is still successful even if it cannot yet:
- take over the TUI
- pick a host
- inspect the registry
- spawn child agents
- expose raw PTY bytes

Those can remain weave-native features.

---

## Suggested implementation order

1. Build a tiny shim process with:
   - ACP server
   - relay client
   - session-id mapping
2. Implement `initialize`
3. Implement `session/new`
4. Implement `session/prompt`
5. Implement `session/update` normalization
6. Implement `session/cancel`
7. Implement permission roundtrip
8. Implement `session/load`
9. Add custom-method placeholders for future weave-specific features

---

## Acceptance criteria

The MVP ACP shim is complete when:

1. An ACP client can initialize successfully.
2. `session/new` creates a working weave-backed session.
3. `session/prompt` reaches the running pi-backed agent.
4. The client receives streamed `session/update` notifications.
5. `session/cancel` interrupts active work.
6. Permission requests round-trip correctly.
7. `session/load` resumes a previously created session through a stable shim-owned handle.
8. Unsupported advanced features fail clearly rather than behaving inconsistently.

---

## Final recommendation

We should treat this as a **small compatibility adapter**, not a platform pivot.

That means:
- keep weave remote control as the real system
- add ACP compatibility only at the session boundary
- resist the urge to force attach/takeover/PTY concepts into ACP too early

If ACP adoption matters later, we can expand from a clean, minimal base instead
of trying to standardize everything at once.
