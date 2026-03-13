# Control Protocol

**Status (2026-03-13):** Tier 0 local protocol is implemented for `initialize`, `session.prompt`, `session.cancel`, `session.agent_ready`, and normalized `session.update` over a local Unix socket. Relay/WebSocket sections remain the planned Tier 1+ shape.

All messages are JSON over WebSocket. Every message has a common envelope.

**ACP alignment note (2026-03-10):** this protocol is still a
relay/control-plane protocol, not a pure Agent Client Protocol clone. But the
client ↔ session lifecycle should intentionally track ACP where possible:
`initialize`, `session.new`, `session.load`, `session.prompt`,
`session.update`, `session.cancel`, and explicit permission mediation. Spawn,
host routing, attach/takeover, PTY transport, and parent/child hierarchy remain
weave-specific extensions.

## Envelope

```jsonc
{
  "id": "msg-uuid",           // unique message ID
  "type": "command|event|error|ack",
  "session_id": "sess-uuid",  // target session (omit for registry-level ops)
  "from": "orch-1|wrapper-abc|human-xyz",
  "timestamp": "2026-02-20T15:04:05Z",
  "payload": { /* type-specific */ }
}
```

## Identity Model

We need to keep four identities distinct.

| Identity | Meaning | Lifetime | Exposed to whom? |
|---|---|---|---|
| `runtime_id` | One wrapper + one live pi-mono process tree | Until process exit | Relay / native clients / ops |
| `session_id` | The weave conversation/session identity used by the relay protocol | Stable across runtime restarts when resumed | Relay clients |
| `persisted_session_handle` | The durable pi-backed resume target (for example a JSONL path or future opaque handle) | Stable across shim / runtime restarts | Relay internals, adapters |
| `client_session_id` | An adapter-facing session id such as an ACP shim session id | Defined by the adapter | Adapter clients only |

Rules:
- A **runtime** may die and be replaced while the **session** survives.
- `session.spawn` creates a new runtime and normally a fresh session.
- `session.load` creates a new runtime attached to an existing persisted session.
- Human attach / takeover changes the control relationship around a runtime/session; it does not create a new session identity.

## 1. Commands (orchestrator/human → relay → wrapper)

### Session / Client Handshake

#### `initialize`
ACP-aligned version + capability negotiation after `auth` succeeds.
```jsonc
{
  "type": "command",
  "payload": {
    "command": "initialize",
    "protocol_version": "weave-rc/0.1",
    "capabilities": {
      "session_prompt": true,
      "session_load": true,
      "session_update": true,
      "session_cancel": true,
      "permission_prompts": true,
      "terminal": true,
      "observe": true,
      "inject": true,
      "takeover": true
    },
    "_meta": {
      "client_name": "huxie-orchestrator",
      "client_version": "0.1.0"
    }
  }
}
```

**Negotiation rule:**
- the client advertises what it can handle
- the server decides what it will enable for this connection
- the `ack.data.capabilities` payload is authoritative for the rest of the session

Clients must not assume that a capability they advertised was accepted.

### Session Lifecycle

#### `session.spawn`
Weave-specific remote creation command. Conceptually this wraps ACP-style
`session.new` plus host selection / process launch.
```jsonc
{
  "type": "command",
  "payload": {
    "command": "session.spawn",
    "host": "host-id",              // target host (or omit for relay to pick)
    "config": {
      "extensions": ["pi-bridge.ts"],
      "system_prompt": "...",       // optional override
      "model": "claude-opus-4-6",   // optional
      "working_dir": "/path",
      "env": { "KEY": "val" }       // optional env vars
    },
    "failure_policy": {
      "strategy": "restart|notify|checkpoint",
      "max_retries": 3,
      "retry_delay_ms": 5000
    },
    "parent_session_id": "sess-parent"  // for hierarchical spawning (optional)
  }
}
```

#### `session.kill`
```jsonc
{
  "payload": {
    "command": "session.kill",
    "reason": "task complete"       // informational
  }
}
```

#### `session.load`
Load or resume a previously persisted pi-mono session on a host.
```jsonc
{
  "type": "command",
  "payload": {
    "command": "session.load",
    "host": "host-id",
    "session_path": "/absolute/path/to/session.jsonl",
    "config": {
      "extensions": ["pi-bridge.ts"],
      "working_dir": "/path"
    }
  }
}
```

### Agent Steering

#### `agent.message`
Send a message into the agent's conversation.

`session.prompt` is the ACP-aligned alias for this command. Both should map to
one internal prompt-turn primitive.
```jsonc
{
  "payload": {
    "command": "agent.message",
    "content": "Please refactor the auth module to use JWT.",
    "role": "orchestrator|human",   // so the extension knows the source
    "priority": "normal|high"       // high = interrupt current tool execution
  }
}
```

#### `agent.cancel`
Cancel the agent's current operation.

`session.cancel` is the ACP-aligned alias for this command.
```jsonc
{
  "payload": {
    "command": "agent.cancel"
  }
}
```

#### `session.permission_response`
Resolve a pending permission request from the agent.
```jsonc
{
  "payload": {
    "command": "session.permission_response",
    "request_id": "perm-uuid",
    "decision": "allow|deny",
    "reason": "optional human or policy note"
  }
}
```

### Attach / Detach

#### `session.attach`
```jsonc
{
  "payload": {
    "command": "session.attach",
    "mode": "observe|inject|takeover",
    "client_id": "human-xyz"
  }
}
```

#### `session.detach`
```jsonc
{
  "payload": {
    "command": "session.detach",
    "client_id": "human-xyz"
  }
}
```

#### `session.escalate` / `session.deescalate`
```jsonc
{
  "payload": {
    "command": "session.escalate",
    "to": "inject|takeover"
  }
}
```

#### `pty.resize`
Resize the remote PTY while attached.
```jsonc
{
  "payload": {
    "command": "pty.resize",
    "rows": 24,
    "cols": 80
  }
}
```

### Registry

#### `registry.list_hosts`
```jsonc
{ "payload": { "command": "registry.list_hosts" } }
```

#### `registry.list_sessions`
```jsonc
{
  "payload": {
    "command": "registry.list_sessions",
    "filter": {
      "host": "host-id",           // optional
      "state": "autonomous",       // optional
      "parent": "sess-parent"      // optional
    }
  }
}
```

#### `registry.host_status`
```jsonc
{ "payload": { "command": "registry.host_status", "host": "host-id" } }
```

---

## 2. Events (wrapper/extension → relay → orchestrator/human)

### Session Events

#### `session.update`
Normalized ACP-inspired umbrella event. Rich clients can still consume the
fine-grained events below, but generic clients should be able to drive their UI
from `session.update` alone.
```jsonc
{
  "type": "event",
  "payload": {
    "event": "session.update",
    "seq": 42,
    "kind": "lifecycle|message_delta|message_complete|tool_begin|tool_end|permission_request|permission_resolved|status|error|complete",
    "run_state": "starting|running|waiting_permission|idle|failed|stopped",
    "delta": {
      "content": "I'll refactor the auth module now..."
    }
  }
}
```

`session.update` is a **closed taxonomy** in V1. New kinds are a protocol
change, not an ad-hoc extension.

Required fields by kind:
- `lifecycle`: `phase`, optionally `from_state`, `to_state`
- `message_delta`: `message_id`, `delta.content`
- `message_complete`: `message_id`, `content`
- `tool_begin`: `tool_call_id`, `tool`
- `tool_end`: `tool_call_id`, `tool`, `status`
- `permission_request`: `request_id`, `tool`, `args`, `timeout_ms`
- `permission_resolved`: `request_id`, `decision`
- `status`: `run_state`, optional summary counters
- `error`: `code`, `message`, optional `recoverable`
- `complete`: final outcome summary

Normalization rules:
- generic clients should not need raw weave event names to function
- tool internals should be summarized, not streamed as arbitrary internal event blobs
- raw PTY output is **not** part of `session.update`; it remains a weave-specific channel

### `session.update` examples

#### `lifecycle`
```jsonc
{
  "type": "event",
  "session_id": "sess-123",
  "payload": {
    "event": "session.update",
    "seq": 1,
    "kind": "lifecycle",
    "run_state": "starting",
    "phase": "agent_boot",
    "from_state": "starting",
    "to_state": "autonomous"
  }
}
```

#### `message_delta`
```jsonc
{
  "type": "event",
  "session_id": "sess-123",
  "payload": {
    "event": "session.update",
    "seq": 2,
    "kind": "message_delta",
    "run_state": "running",
    "message_id": "msg-1",
    "delta": {
      "content": "I'll start by reading the auth module"
    }
  }
}
```

#### `message_complete`
```jsonc
{
  "type": "event",
  "session_id": "sess-123",
  "payload": {
    "event": "session.update",
    "seq": 3,
    "kind": "message_complete",
    "run_state": "running",
    "message_id": "msg-1",
    "content": "I'll start by reading the auth module and then refactor the token handling."
  }
}
```

#### `tool_begin`
```jsonc
{
  "type": "event",
  "session_id": "sess-123",
  "payload": {
    "event": "session.update",
    "seq": 4,
    "kind": "tool_begin",
    "run_state": "running",
    "tool_call_id": "tool-1",
    "tool": "read",
    "args": {
      "path": "/workspace/server/auth.go"
    }
  }
}
```

#### `tool_end`
```jsonc
{
  "type": "event",
  "session_id": "sess-123",
  "payload": {
    "event": "session.update",
    "seq": 5,
    "kind": "tool_end",
    "run_state": "running",
    "tool_call_id": "tool-1",
    "tool": "read",
    "status": "completed",
    "duration_ms": 42
  }
}
```

#### `permission_request`
```jsonc
{
  "type": "event",
  "session_id": "sess-123",
  "payload": {
    "event": "session.update",
    "seq": 6,
    "kind": "permission_request",
    "run_state": "waiting_permission",
    "request_id": "perm-1",
    "tool": "bash",
    "args": {
      "command": "go test ./..."
    },
    "timeout_ms": 30000
  }
}
```

#### `permission_resolved`
```jsonc
{
  "type": "event",
  "session_id": "sess-123",
  "payload": {
    "event": "session.update",
    "seq": 7,
    "kind": "permission_resolved",
    "run_state": "running",
    "request_id": "perm-1",
    "decision": "allow"
  }
}
```

#### `status`
```jsonc
{
  "type": "event",
  "session_id": "sess-123",
  "payload": {
    "event": "session.update",
    "seq": 8,
    "kind": "status",
    "run_state": "running",
    "summary": {
      "tool_calls": 3,
      "input_tokens": 1200,
      "output_tokens": 450
    }
  }
}
```

#### `error`
```jsonc
{
  "type": "event",
  "session_id": "sess-123",
  "payload": {
    "event": "session.update",
    "seq": 9,
    "kind": "error",
    "run_state": "failed",
    "code": "context_window_exceeded",
    "message": "Token limit reached",
    "recoverable": true
  }
}
```

#### `complete`
```jsonc
{
  "type": "event",
  "session_id": "sess-123",
  "payload": {
    "event": "session.update",
    "seq": 10,
    "kind": "complete",
    "run_state": "stopped",
    "outcome": "completed",
    "summary": {
      "tool_calls": 4,
      "messages": 2
    }
  }
}
```

### Fine-grained session / agent events

#### `session.state_changed`
```jsonc
{
  "type": "event",
  "payload": {
    "event": "session.state_changed",
    "from_state": "starting",
    "to_state": "autonomous",
    "trigger": "agent.ready"
  }
}
```

#### `session.agent_ready`
```jsonc
{
  "payload": {
    "event": "session.agent_ready",
    "pid": 12345,
    "extensions_loaded": ["pi-bridge.ts"]
  }
}
```

### Conversation Events

#### `agent.assistant_message`
```jsonc
{
  "payload": {
    "event": "agent.assistant_message",
    "content": "I'll refactor the auth module now...",
    "tokens": { "input": 1200, "output": 450 }
  }
}
```

#### `agent.tool_call`
```jsonc
{
  "payload": {
    "event": "agent.tool_call",
    "tool": "bash",
    "args": { "command": "npm test" },
    "status": "started|completed|failed",
    "result": "...",                // when completed
    "duration_ms": 3200             // when completed
  }
}
```

#### `agent.tool_permission`
Agent is asking for permission to execute a tool.

`session.request_permission` is the ACP-aligned umbrella name for this flow.
```jsonc
{
  "payload": {
    "event": "agent.tool_permission",
    "request_id": "perm-uuid",
    "tool": "bash",
    "args": { "command": "rm -rf /tmp/build" },
    "timeout_ms": 30000             // auto-deny after timeout
  }
}
```

Permission invariants:
1. `request_id` is unique within the session.
2. A permission request may be resolved exactly once.
3. Timeout resolves the request as deterministic `deny` if no decision arrives.
4. Only one authority may answer at a time for a session: human in takeover/inject,
   otherwise the orchestrator or server policy layer.
5. Every resolved request must produce either a fine-grained follow-up event or a
   normalized `session.update(kind=permission_resolved)`.
6. A canceled or failed run implicitly closes all outstanding permission requests.

#### `agent.error`
```jsonc
{
  "payload": {
    "event": "agent.error",
    "error": "context_window_exceeded",
    "message": "Token limit reached",
    "recoverable": true
  }
}
```

### Metrics Events

#### `agent.metrics`
Periodic heartbeat with stats.
```jsonc
{
  "payload": {
    "event": "agent.metrics",
    "uptime_s": 3600,
    "total_tokens": { "input": 50000, "output": 12000 },
    "tool_calls": 42,
    "errors": 1,
    "current_task": "refactoring auth module"
  }
}
```

### PTY Events (for human attach)

#### `pty.output`
Raw terminal output chunk (only sent to attached human in observe/inject mode).
```jsonc
{
  "payload": {
    "event": "pty.output",
    "data": "base64-encoded-terminal-output"
  }
}
```

#### `pty.input`
Raw terminal input from human (only in takeover mode).
```jsonc
{
  "payload": {
    "command": "pty.input",
    "data": "base64-encoded-keystrokes"
  }
}
```

### Registry Events

#### `registry.host_joined` / `registry.host_left`
```jsonc
{
  "payload": {
    "event": "registry.host_joined",
    "host_id": "host-abc",
    "capabilities": { "cpus": 8, "mem_gb": 32 }
  }
}
```

---

## 3. Errors

```jsonc
{
  "type": "error",
  "payload": {
    "code": "SESSION_NOT_FOUND|ATTACH_CONFLICT|HOST_UNAVAILABLE|AUTH_FAILED|INVALID_COMMAND",
    "message": "Human-readable description",
    "ref_id": "msg-uuid-that-caused-this"   // the command that triggered the error
  }
}
```

### Error Categories

External clients should think in these categories first:
- `auth`
- `unsupported`
- `invalid_state`
- `not_found`
- `unavailable`
- `permission_denied`
- `internal`

Weave-native error codes can be preserved in metadata, but clients should not be
forced to understand every weave-specific code.

### Error Codes

| Code | Category | Meaning |
|------|----------|---------|
| `SESSION_NOT_FOUND` | `not_found` | Session ID doesn't exist or is STOPPED |
| `ATTACH_CONFLICT` | `invalid_state` | Another controller is already attached |
| `HOST_UNAVAILABLE` | `unavailable` | Target host is not registered or unreachable |
| `AUTH_FAILED` | `auth` | Invalid or expired token |
| `INVALID_COMMAND` | `unsupported` | Malformed or unknown command |
| `INVALID_STATE` | `invalid_state` | Command not valid in current session state (e.g. inject while not attached) |
| `SPAWN_FAILED` | `internal` | Host daemon failed to launch wrapper+pi-mono |
| `RATE_LIMITED` | `unavailable` | Too many commands in short period |

---

## 4. Acknowledgments

Every command receives an ack:
```jsonc
{
  "type": "ack",
  "payload": {
    "ref_id": "msg-uuid-of-command",
    "status": "ok|rejected",
    "reason": "...",                // if rejected
    "data": {                         // optional command-specific result
      "session_id": "sess-uuid",
      "capabilities": {
        "session_update": true
      }
    }
  }
}
```

---

## 5. Auth

All WebSocket connections start with a handshake:
```jsonc
// Client sends on connect:
{
  "type": "command",
  "payload": {
    "command": "auth",
    "token": "bearer-token",
    "role": "orchestrator|daemon|human",
    "identity": "orch-1|daemon-host-abc|human-victor"
  }
}
// Relay responds with ack or AUTH_FAILED error.
// After auth succeeds, clients SHOULD send `initialize` before any
// session-scoped commands.
```

---

## 6. Protocol Conventions

1. **Message ordering**: relay preserves per-session ordering. Events from one session arrive in order.
2. **`session.update.seq` ordering**: `seq` is strictly increasing per session and never reused.
3. **Idempotency**: commands include a unique `id`. Relay deduplicates.
4. **Heartbeat**: clients send `{"type":"command","payload":{"command":"ping"}}` every 30s. Relay responds with ack. Timeout after 90s → disconnect.
5. **Backpressure**: if a client falls behind on events, relay drops oldest events and sends a `gap` event with the count of dropped messages.
6. **Reconnect/resume in V1**: reconnect is best-effort within the relay buffer window. Clients must tolerate gaps unless an explicit durable replay contract exists for that surface.
7. **Capability authority**: after `initialize`, the server-accepted capability set is authoritative.
8. **Session vs runtime**: commands target a stable `session_id`; runtimes may restart under that session according to failure policy.
