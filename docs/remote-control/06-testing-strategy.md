# Testing Strategy

**Status (2026-03-14):** Tier 0 and Tier 1 coverage exist in Go tests (`remotecontrol/protocol`, `remotecontrol/local`, `remotecontrol/relay`) and have been smoke-tested against a real local pi process both directly and through the relay for init/prompt, with cancel validated in the local path. Tier 2 now also has relay tests and manual smoke coverage for `session.spawn`, `runtime.stop`, `session.load`, `session.status`, and `registry.list_sessions` against real pi session persistence. Tier 3 has test coverage for explicit delivery mapping, fake-pi-backed local and relay permission request/response loops, richer confirm-style permission lifecycle cases (status visibility plus stale/duplicate response rejection), and reproducible real relay smoke coverage via `testdata/pi/permission_fixture.ts` and `scripts/remotecontrol-permission-smoke.sh`. Tier 4 adds relay tests for observe conflict, same-identity observe→inject mode escalation, inject visibility, permission authority while attached, and a real observe/watch/inject smoke script at `scripts/remotecontrol-attach-smoke.sh`. Tier 5 now adds relay tests for takeover queueing, takeover-held permission authority, initial PTY output/input/resize forwarding via a scripted helper runtime, plus smoke scripts at `scripts/remotecontrol-takeover-queue-smoke.sh`, `scripts/remotecontrol-pty-smoke.sh`, and `scripts/remotecontrol-real-pi-pty-smoke.sh`.

## Three Test Tiers

```
┌─────────────────────────────────────────────────────────────────┐
│  Tier 1: FAKE PI (Go)                                          │
│  Speed: milliseconds    Cost: zero    Runs: every commit        │
│                                                                 │
│  Go fake process speaks the pi RPC protocol used by the wrapper.│
│  Tests: wrapper, relay, host daemon, state machine, protocol.   │
│  No Node.js, no LLM, no real pi runtime.                        │
├─────────────────────────────────────────────────────────────────┤
│  Tier 2: REAL PI-MONO + MOCK MODEL                              │
│  Speed: seconds         Cost: zero    Runs: CI / pre-merge      │
│                                                                 │
│  Real pi-mono with a local mock model (canned responses).       │
│  Tests: real extension code, real pi-mono lifecycle,             │
│         message injection fidelity, event coverage.             │
├─────────────────────────────────────────────────────────────────┤
│  Tier 3: REAL LLM                                               │
│  Speed: 10-60s/test     Cost: API $   Runs: nightly / release   │
│                                                                 │
│  Real pi-mono + real Claude API.                                │
│  Tests: end-to-end task completion, steering behavior,          │
│         tool execution, real conversation flows.                │
└─────────────────────────────────────────────────────────────────┘
```

---

## Tier 1: Fake pi (`weave-fakepi`)

A Go binary that impersonates pi from the wrapper's perspective.

### What it does

1. **Speaks** the pi RPC JSONL protocol over stdin/stdout.
2. **Accepts** commands (`prompt`, `abort`, `get_state`, etc.).
3. **Emits** events (`agent_start`, `message_update`, `tool_execution_*`, etc.) on a scripted schedule.
4. **Simulates** slow runs, aborts, crashes, and malformed output.
5. **Lets** wrapper/protocol tests run without Node.js, auth, or a real model.

### Interface

```go
type FakePiMono struct {
    // Script drives behavior: sequence of actions the fake takes
    Script    []ScriptEntry

    // Received commands from the wrapper (for assertions)
    Received  []BridgeMessage

    // Control
    CrashAfter  time.Duration  // simulate crash (kill self after N)
    SlowMode    time.Duration  // add delay to responses
    HangOnTool  string         // hang when receiving specific tool (test cancel)
}

type ScriptEntry struct {
    // What triggers this entry
    Trigger   Trigger  // OnMessage, OnConnect, AfterDelay, OnPTYInput

    // What the fake does
    Actions   []Action // EmitEvent, WritePTY, SendBridgeMessage, Sleep, Crash, Hang
}
```

### Scripted scenarios

| Scenario | Purpose |
|----------|---------|
| `happy-path` | Agent ready → receive message → emit assistant response → emit tool call → emit tool result → done |
| `initialize-capabilities` | Client authenticates, sends `initialize`, negotiates capabilities, then starts a session. Tests ACP-aligned handshake. |
| `multi-turn` | Three rounds of orchestrator messages with assistant responses. Tests conversational steering. |
| `slow-tool` | Tool execution takes 10s. Tests cancel behavior and steer-during-tool. |
| `crash-mid-task` | Agent crashes after 2nd message. Tests failure detection and recovery. |
| `pty-echo` | Writes PTY output, reads PTY input, echoes back. Tests human attach round-trip. |
| `event-flood` | Emits 1000 events/second. Tests backpressure and relay buffering. |
| `hang` | Stops responding after ready. Tests heartbeat timeout detection. |
| `large-output` | Emits a 100KB assistant message in chunks. Tests streaming and buffering. |

### What this tests

- **Wrapper**: RPC process management, local socket protocol, relay connection, event forwarding.
- **Relay**: Session state machine, routing, attach locks, event buffering, error handling.
- **Host daemon**: Process lifecycle, spawn/kill, crash detection, restart logic.
- **Protocol**: Serialization, envelope handling, ack/error flows, idempotency.
- **State machine**: All transitions, invalid transition rejection, concurrent operations.

### What this does NOT test

- Real pi-mono extension behavior (message injection, event extraction).
- Real LLM interaction.
- Pi-mono's actual PTY/TUI rendering.

---

## Tier 2: Real Pi-mono + Mock Model

### Mock model

Pi-mono supports custom model providers. We create a mock provider that:
- Returns deterministic responses based on input patterns
- Simulates tool-use responses (returns tool call blocks)
- Supports configurable latency
- Optionally records all prompts for assertion

Implementation options (needs investigation):
1. **Pi-mono's built-in mock** — check if pi-mono has a test/mock model provider
2. **Local HTTP server** — a small Go/TS server that implements the Anthropic API shape, returns canned responses. Pi-mono points at it as a custom endpoint.
3. **Extension override** — extension intercepts at the `context` event (before LLM call) and returns a synthetic response

### What this tests

- **pi RPC integration**: Real pi startup, command/response handling, and streamed events in RPC mode.
- **Pi lifecycle**: Real startup, session loading, and shutdown.
- **Event fidelity**: Do the events we receive from pi RPC match what pi actually does?
- **Session resume**: Real JSONL persistence, `--session <path>` reload.

### Scenarios

| Scenario | Purpose |
|----------|---------|
| `inject-idle` | Send message when agent is idle → verify it enters conversation and triggers response |
| `inject-steer` | Send steer message during tool execution → verify tool is interrupted |
| `inject-followup` | Send followUp message during tool execution → verify delivery after tools complete |
| `event-coverage` | Trigger all event types → verify pi RPC emits each one and wrapper normalization stays stable |
| `session-resume` | Start session, crash, restart with `--session` → verify history is loaded |
| `permission-roundtrip` | Agent requests tool permission → client responds allow/deny → verify wrapper/runtime behavior |
| `rpc-startup` | Verify wrapper bootstrap (`get_state`), ready handshake, and runtime metadata |

### Deterministic real permission smoke

To make the real permission flow reproducible, keep a tiny fixture extension in the repo:

- `testdata/pi/permission_fixture.ts`
  - registers `/permtest`
  - calls `ctx.ui.confirm(...)`
  - sends `PERMISSION_ALLOWED` or `PERMISSION_DENIED`

Run the end-to-end relay smoke with:

```bash
./scripts/remotecontrol-permission-smoke.sh
```

Or a single branch of the flow:

```bash
./scripts/remotecontrol-permission-smoke.sh allow
./scripts/remotecontrol-permission-smoke.sh deny
```

What the script verifies:

1. `weave-wrapper` loads the fixture extension into a real pi RPC process.
2. `weave-inspect relay ... prompt "/permtest"` receives a normalized `permission_request`.
3. `weave-inspect relay ... allow|deny <request-id>` resolves the request.
4. The prompt completes with the expected final output token.

This avoids relying on model-specific tool-choice behavior just to trigger a confirm dialog.

### Real observe / watch / inject smoke

Tier 4 behavior is reproducible with:

```bash
./scripts/remotecontrol-attach-smoke.sh
```

What it verifies:

1. `weave-inspect relay watch` can passively stream a session without taking the attach lock
2. a human client can `session.attach` in `observe` mode and appear in `session.status`
3. the same human identity can reconnect and escalate from `observe` to `inject`
4. the watch stream sees both the injected output and the explicit `mode_changed` attachment status event

### Real takeover queue smoke

Initial Tier 5 control-plane behavior is reproducible with:

```bash
./scripts/remotecontrol-takeover-queue-smoke.sh
```

What it verifies:

1. a human client can `session.attach` in `takeover` mode
2. orchestrator prompts are acknowledged as queued instead of being sent to the runtime immediately
3. `session.status` reports the queued prompt count while takeover is active
4. detaching the takeover human flushes the queued prompt back to the runtime

### Scripted PTY byte-transport smoke

The first PTY transport slice is reproducible with:

```bash
./scripts/remotecontrol-pty-smoke.sh
```

What it verifies:

1. a PTY-backed wrapper runtime can connect through the relay
2. a human in `takeover` mode receives raw `pty.output`
3. the same human identity can send `pty-input`
4. `pty-resize` is accepted while takeover is active

This uses the tracked echo helper at `testdata/pty/echo.py`.

### Real interactive pi takeover smoke

The same PTY path is also reproducible against a real interactive `pi` process with:

```bash
./scripts/remotecontrol-real-pi-pty-smoke.sh
```

What it verifies:

1. wrapper PTY mode can launch a real `pi` child process
2. a human in `takeover` mode can attach to that live interactive `pi` runtime
3. `pty-input` can drive a real TUI command (`/session` + Enter)
4. `pty-resize` updates are reflected in `session.status`

### Manual relay-managed PTY spawn/load validation

I also manually validated the new transport-selection path with relay-managed launch:

```bash
weave-inspect relay --session <id> --transport pty spawn
weave-inspect relay --session <id> attach takeover
weave-inspect relay --session <id> pty-input $'/session\r'
weave-inspect relay --session <id> kill-runtime
weave-inspect relay --session <id> --transport pty load
```

That confirms `session.spawn` / `session.load` can now choose the real interactive PTY-backed pi runtime instead of always defaulting to RPC.

### Manual best-UX takeover validation

I also manually validated the more ergonomic takeover flow:

```bash
weave-inspect relay --session <id> spawn                 # defaults to rpc
weave-inspect relay --session <id> takeover              # auto-upgrades to pty when resumable
weave-inspect relay --session <id> pty-input $'/session\r'
weave-inspect relay --session <id> release
weave-inspect relay --session <id> switch-transport rpc
```

That confirms the explicit runtime-switch primitive works and that human takeover can now be driven by intent (`takeover`) rather than by manually choosing transport first.

I also validated the reattach blank-screen fix by running takeover with a local terminal size available (or `LINES` / `COLUMNS` fallback) and confirming the session immediately recorded non-zero `pty_rows` / `pty_cols` without needing a second manual `pty-resize` command.

Finally, I validated live stdin passthrough by piping `/session\r` plus the local disconnect escape (`Ctrl-]`, `\x1d`) into `weave-inspect relay takeover`, then confirming the PTY stream showed `/session` and the session status recorded the auto-established PTY size. The client also supports a second local detach escape, `~.` at line start, for terminals where `Ctrl-]` is awkward.

On Zellij/modern terminal setups, I also confirmed that `Ctrl-]` may arrive as the extended CSI-u sequence `ESC [ 93 ; 5 u` instead of raw `0x1d`; takeover now treats that CSI-u form as the same local disconnect escape.

To improve reattach rendering, takeover now also performs a resize-bump-and-restore after applying the real terminal size. This is a pragmatic redraw trigger for TUIs that only repaint fully when they receive a resize event.

For takeover lifecycle, relay tests now also cover automatic return to RPC when a session was auto-upgraded from RPC into PTY for takeover and the human later releases or disconnects.

For day-to-day operator ergonomics, `weave-inspect relay` now remembers the last successful relay context per repository in the local user cache. In practice that means the first command may still be fully specified, but later follow-ups can usually omit `--relay`, `--token`, `--session`, and `--identity`.

There is also now a thin operator TUI:

```bash
weave-inspect relay tui
```

It uses the saved relay context, lists sessions, and supports simple keyboard-driven actions such as spawning a new session, selecting one, taking over, releasing, prompting, loading, and stopping runtimes. It is implemented with Bubble Tea so the UI can hand terminal control to child commands and then resume cleanly, which avoids the stdin contention bugs that a hand-rolled raw-input loop hit.

---

## Tier 3: Real LLM (Integration)

Small set of end-to-end tests with real Claude API calls. Run sparingly.

### Scenarios

| Scenario | Purpose |
|----------|---------|
| `steering-flow` | Orchestrator spawns agent → sends task → agent works → orchestrator redirects mid-task → agent adjusts |
| `tool-cycle` | Agent reads a file, edits it, runs tests. Verify tool events captured end-to-end. |
| `attach-observe` | Agent running autonomously. Inspector attaches, observes live events. |
| `crash-recovery` | Agent working. Kill wrapper. Host daemon restarts. Agent resumes from JSONL. |

---

## Dev Inspector CLI (`weave-inspect`)

A Go CLI tool for humans to debug and observe the system. Connects to the relay as a client.

### Commands

```
weave-inspect hosts                     # list registered hosts
weave-inspect sessions                  # list all sessions with state
weave-inspect session <id>              # show session detail (state, config, parent, host)
weave-inspect events <session-id>       # tail live events (like `tail -f`)
weave-inspect events <session-id> \
    --filter type=agent.tool_call       # filter by event type
weave-inspect events <session-id> \
    --since 5m                          # replay last 5 minutes from buffer
weave-inspect state <session-id>        # show current state machine state + transition history
weave-inspect attach <session-id>       # attach in observe mode (raw event stream)
weave-inspect send <session-id> \
    --message "do X"                    # inject a message (for debugging)
weave-inspect kill <session-id>         # kill a session
weave-inspect relay-stats               # relay health: connections, buffer usage, uptime
```

### Output modes

- **`--format text`** (default): Human-readable, colored terminal output.
- **`--format json`**: Machine-readable NDJSON. Pipe to `jq`, log to file, etc.
- **`--follow`** / `-f`: Continuous streaming (for `events` and `attach` commands).

### Example usage during development

```bash
# Terminal 1: start relay
weave-relay --addr :8080 --token dev-token

# Terminal 2: start a fake agent for testing
weave-wrapper --relay ws://localhost:8080 --token dev-token --fake happy-path

# Terminal 3: inspect
weave-inspect sessions
# → sess-abc123  AUTONOMOUS  host-local  uptime=12s  events=5

weave-inspect events sess-abc123 -f
# → 15:04:01 session.agent_ready     pid=12345
# → 15:04:02 agent.assistant_message  "I'll start by reading the file..."
# → 15:04:03 agent.tool_call          bash: cat main.go  [started]
# → 15:04:04 agent.tool_call          bash: cat main.go  [completed 320ms]
# → ...

weave-inspect state sess-abc123
# → Current: AUTONOMOUS
# → History:
# →   15:04:00 STARTING → AUTONOMOUS  (trigger: agent.ready)

weave-inspect send sess-abc123 --message "stop and summarize what you've done"
# → ack: ok

weave-inspect events sess-abc123 -f --filter type=agent.error
# (waits for errors...)
```

---

## Test Harness Per Phase

### Phase 1 (Wrapper + Extension)

| Harness | What |
|---------|------|
| `fakepimono` binary | Go fake for wrapper unit/integration tests |
| `bridge_test.go` | Tests for bridge protocol serialization, connection, reconnection |
| `pty_test.go` | Tests for PTY creation, read/write, resize |
| Tier 2 smoke test | Real pi-mono + mock model, one inject-and-respond cycle |

### Phase 2 (Relay)

| Harness | What |
|---------|------|
| `relay_test.go` | State machine transitions, routing, auth, buffering. Uses `fakepimono` + fake wrapper clients. |
| `protocol_test.go` | Serialization round-trips for all message types. Fuzz testing on envelope parsing. |
| `weave-inspect` v1 | `hosts`, `sessions`, `events`, `state` commands |

### Phase 3 (Host Daemon)

| Harness | What |
|---------|------|
| `daemon_test.go` | Spawn/kill lifecycle, crash detection, restart. Uses `fakepimono`. |
| `integration_test.go` | Full stack: relay + daemon + wrapper + fakepimono. Spawn via relay, steer, kill. |
| Chaos scenarios | Kill wrapper mid-task, kill daemon, disconnect relay, corrupt bridge messages |

### Phase 4 (Human Attach)

| Harness | What |
|---------|------|
| `attach_test.go` | Lock acquisition, mode switching, PTY forwarding. Uses `fakepimono` with `pty-echo` scenario. |
| `weave-inspect` v2 | `attach` and `send` commands. Used for manual testing of observe/inject/takeover. |
| Manual test script | Step-by-step script: start agent, attach observe, escalate inject, escalate takeover, type commands, deescalate, detach. |

### Phase 5+ (Web UI, Subagents)

| Harness | What |
|---------|------|
| Browser tests | Playwright or similar for web UI attach/observe |
| `subagent_test.go` | Hierarchical spawn, parent-child lifecycle, cascading kill |
| Tier 3 suite | Real LLM end-to-end scenarios |

---

## Fake Pi-mono Bridge Protocol

The bridge protocol between wrapper and extension (over Unix domain socket) is the key contract. Both the real extension and the Go fake implement it. Documenting it here so both sides agree.

### Message format

NDJSON (newline-delimited JSON) over Unix domain socket. Each line is one message.

```jsonc
// Wrapper → Extension (commands)
{"type": "command", "id": "cmd-1", "command": "agent.message", "payload": {...}}
{"type": "command", "id": "cmd-2", "command": "agent.cancel", "payload": {}}
{"type": "ping"}

// Extension → Wrapper (events + acks)
{"type": "event", "event": "session.agent_ready", "payload": {...}}
{"type": "event", "event": "agent.assistant_message", "payload": {...}}
{"type": "ack", "ref_id": "cmd-1", "status": "ok"}
{"type": "pong"}
```

### Handshake

1. Wrapper creates socket at `$WV_BRIDGE_SOCKET` (env var passed to pi-mono).
2. Extension connects on startup.
3. Extension sends `{"type": "event", "event": "session.agent_ready", "payload": {"extensions": [...]}}`.
4. Wrapper considers the bridge live.

### Heartbeat

Extension sends `pong` in response to wrapper's `ping`. Wrapper pings every 10s. If no pong within 30s → wrapper considers extension dead → session → FAILED.

---

## What We Are NOT Testing

- Pi-mono internals (their responsibility, not ours)
- LLM response quality (not a correctness concern for the control plane)
- Network partitions between machines (V1 scope: single-machine or trusted LAN)
- Browser compatibility (defer to Phase 5)
- Performance under >100 concurrent sessions (defer to scale testing)
