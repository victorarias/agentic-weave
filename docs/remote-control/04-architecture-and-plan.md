# V1 Architecture and Phased Plan

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
│                        WV-RELAY (Go)                        │
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
│  WV-DAEMON (Go)    │             │  HUMAN CLIENT           │
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
│  WV-WRAPPER (Go, one per agent)                 │
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
│  │  │  wv-bridge extension (TS)    │  │   │
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

## Wrapper ↔ Extension Bridge

The wrapper (Go) and extension (TS, inside pi-mono) need a local communication channel. Options:

| Option | Pros | Cons |
|--------|------|------|
| Unix domain socket | Bidirectional, fast, well-supported | Need to coordinate socket path |
| Localhost TCP | Simple, debuggable with curl/netcat | Port management |
| Stdin pipe passthrough | Pi-mono already reads stdin | Conflicts with interactive mode |
| Named pipe (FIFO) | Simple, no networking | Unidirectional (need two) |

**Recommendation for v1**: Unix domain socket. The wrapper creates it at a known path (e.g. `/tmp/wv-<session-id>.sock`), passes the path to pi-mono via env var, the extension connects on startup.

## Phased Implementation Plan

### Phase 1: Local Wrapper + Extension (weeks 1-2)

**Goal**: One wrapper controlling one pi-mono instance locally. No relay, no network.

Deliverables:
1. **Go wrapper** that:
   - Spawns pi-mono in interactive mode with a virtual PTY
   - Creates a Unix domain socket for the bridge
   - Reads/writes the PTY (for future human attach)
   - Sends commands to the extension via the bridge
   - Receives events from the extension via the bridge

2. **TypeScript extension** (`wv-bridge.ts`) that:
   - Connects to the Unix domain socket on startup
   - Receives `agent.message` commands → injects into pi-mono conversation
   - Receives `agent.cancel` → cancels current operation
   - Emits `agent.assistant_message`, `agent.tool_call`, `agent.error` events
   - Emits `session.agent_ready` on startup

3. **Smoke test**: wrapper sends a message, extension injects it, pi-mono responds, extension streams the response back, wrapper prints it.

**No relay, no auth, no registry. Just the core loop.**

### Phase 2: Relay + Registry (weeks 3-4)

**Goal**: Multiple wrappers connecting through a central relay. Orchestrator can discover and steer agents.

Deliverables:
1. **Go relay server** with:
   - WebSocket endpoint
   - Token-based auth handshake
   - Session registry (in-memory)
   - Host registry (in-memory)
   - Message routing (orchestrator → session, session → orchestrator)
   - Event buffering (ring buffer per session)
   - State machine enforcement (valid transitions only)

2. **Wrapper updated** to:
   - Connect to relay on startup
   - Register its session
   - Receive commands via relay (not just local bridge)
   - Forward events to relay

3. **Orchestrator interface spec** (for the existing Go service):
   - WebSocket client connecting to relay
   - Send: `session.spawn`, `agent.message`, `session.kill`, `registry.*`
   - Receive: all session events
   - Handle: acks and errors

4. **Smoke test**: orchestrator spawns an agent via relay, sends a message, gets the response back.

### Phase 3: Host Daemon + Dynamic Spawn (weeks 5-6)

**Goal**: Orchestrator can spawn agents on remote machines.

Deliverables:
1. **Go host daemon** that:
   - Connects to relay, registers as a host
   - Receives `session.spawn` commands
   - Launches wrapper+pi-mono pairs
   - Monitors child processes (detects crashes)
   - Reports `registry.host_joined` / `registry.host_left`

2. **Relay updated** to:
   - Route spawn commands to host daemons
   - Track host → session mapping
   - Handle host disconnection (mark sessions as FAILED)

3. **Failure handling**:
   - Configurable failure policies per session
   - Relay detects wrapper disconnect → evaluates policy
   - Auto-restart: relay tells host daemon to respawn

4. **Smoke test**: orchestrator spawns agent on a remote host, steers it, kills it.

### Phase 4: Human Attach (weeks 7-8)

**Goal**: A human can attach to a running session, observe, inject, and take over.

Deliverables:
1. **Attach lock manager** in relay:
   - Single-attach enforcement
   - Mode tracking (observe/inject/takeover)
   - Escalation/de-escalation

2. **Wrapper updated** to:
   - Forward PTY output to attached human (via relay)
   - Accept PTY input from human (in takeover mode)
   - Pause orchestrator input during takeover

3. **CLI attach client** (Go):
   - Connects to relay, sends `session.attach`
   - Renders PTY output in terminal (raw mode passthrough)
   - Forwards keystrokes as `pty.input` in takeover mode
   - Supports mode switching (observe → inject → takeover)

4. **Smoke test**: agent running autonomously, human attaches, observes, escalates to takeover, types commands, de-escalates, detaches.

### Phase 5: Web UI + Observability (weeks 9-10)

**Goal**: Browser-based session inspection and attach.

Deliverables:
1. **Web UI** (framework TBD):
   - Session list (from registry)
   - Live event stream with filtering
   - Attach with observe/inject modes
   - xterm.js terminal for takeover mode
   - Metrics dashboard (tokens, tool calls, errors)

2. **Event filtering** in relay:
   - Clients can subscribe with filters (event type, severity)
   - Reduces bandwidth for observe-only clients

### Phase 6: Hierarchical Subagents (week 11+)

**Goal**: Agents can spawn and steer child agents.

Deliverables:
1. **Extension updated** to:
   - Expose a `spawn_subagent` tool to pi-mono
   - Route spawn requests through the wrapper → relay
   - Track parent-child relationships

2. **Relay updated** to:
   - Track session hierarchy (parent_session_id)
   - Propagate kill to children (optional, configurable)
   - Allow parent sessions to query child status

## What the Orchestrator Needs to Implement

The existing Go service needs a new module that:

1. **Connects** to the relay via WebSocket.
2. **Authenticates** with a token (`auth` command).
3. **Discovers** available hosts (`registry.list_hosts`).
4. **Spawns** agents on hosts (`session.spawn`) with:
   - System prompt / task description
   - Failure policy
   - Extension configuration
5. **Steers** agents conversationally (`agent.message`) — reads responses, decides next message.
6. **Monitors** sessions via event stream (subscribes to events on spawn).
7. **Handles** failures (receives `session.state_changed` to FAILED, decides whether to retry).
8. **Manages** attach requests from humans (optional: orchestrator can observe who's attached).
9. **Kills** sessions when tasks complete (`session.kill`).

The orchestrator does NOT manage PTYs, host daemons, or pi-mono internals. It only speaks the control protocol.
