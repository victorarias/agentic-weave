# Control Protocol

All messages are JSON over WebSocket. Every message has a common envelope.

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

## 1. Commands (orchestrator/human → relay → wrapper)

### Session Lifecycle

#### `session.spawn`
```jsonc
{
  "type": "command",
  "payload": {
    "command": "session.spawn",
    "host": "host-id",              // target host (or omit for relay to pick)
    "config": {
      "extensions": ["wv-bridge.ts"],
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

### Agent Steering

#### `agent.message`
Send a message into the agent's conversation.
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
```jsonc
{
  "payload": {
    "command": "agent.cancel"
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
    "extensions_loaded": ["wv-bridge.ts"]
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
```jsonc
{
  "payload": {
    "event": "agent.tool_permission",
    "tool": "bash",
    "args": { "command": "rm -rf /tmp/build" },
    "timeout_ms": 30000             // auto-deny after timeout
  }
}
```

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

### Error Codes

| Code | Meaning |
|------|---------|
| `SESSION_NOT_FOUND` | Session ID doesn't exist or is STOPPED |
| `ATTACH_CONFLICT` | Another controller is already attached |
| `HOST_UNAVAILABLE` | Target host is not registered or unreachable |
| `AUTH_FAILED` | Invalid or expired token |
| `INVALID_COMMAND` | Malformed or unknown command |
| `INVALID_STATE` | Command not valid in current session state (e.g. inject while not attached) |
| `SPAWN_FAILED` | Host daemon failed to launch wrapper+pi-mono |
| `RATE_LIMITED` | Too many commands in short period |

---

## 4. Acknowledgments

Every command receives an ack:
```jsonc
{
  "type": "ack",
  "payload": {
    "ref_id": "msg-uuid-of-command",
    "status": "ok|rejected",
    "reason": "..."                 // if rejected
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
```

---

## 6. Protocol Conventions

1. **Message ordering**: relay preserves per-session ordering. Events from one session arrive in order.
2. **Idempotency**: commands include a unique `id`. Relay deduplicates.
3. **Heartbeat**: clients send `{"type":"command","payload":{"command":"ping"}}` every 30s. Relay responds with ack. Timeout after 90s → disconnect.
4. **Backpressure**: if a client falls behind on events, relay drops oldest events and sends a `gap` event with the count of dropped messages.
5. **Reconnection**: clients reconnect with `last_event_id` to resume from where they left off (within the relay's buffer window).
