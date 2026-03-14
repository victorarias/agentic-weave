# Session State Machine

## States

```
                         spawn
            ┌───────────────────────────────┐
            ▼                               │
        ┌────────┐   agent ready        ┌───┴─────┐
        │STARTING├─────────────────────►│AUTONOMOUS│◄──────────────┐
        └───┬────┘                      └──┬───┬───┘               │
            │                              │   │                   │
            │ spawn fails                  │   │ human attaches    │ human detaches
            ▼                              │   ▼                   │
        ┌───────┐                          │ ┌─────────┐           │
        │FAILED │◄─────────────────────────┤ │OBSERVING├───────────┤
        └───┬───┘   unrecoverable error    │ └──┬──────┘           │
            │                              │    │                  │
            │ retry (if policy allows)     │    │ escalate         │
            │                              │    ▼                  │
            └──────────────────────────────┤ ┌──────────┐          │
                                           │ │INJECTING ├──────────┤
                                           │ └──┬───────┘          │
                                           │    │                  │
                                           │    │ escalate         │
                                           │    ▼                  │
                                           │ ┌─────────┐           │
                                           │ │TAKEOVER ├───────────┘
                                           │ └─────────┘
                                           │
                                           │ task complete / kill
                                           ▼
                                       ┌────────┐
                                       │STOPPED │
                                       └────────┘
```

## State Definitions

### STARTING
- Wrapper process launched, pi-mono initializing.
- Extension connecting to wrapper's bridge.
- Not yet accepting commands.
- Timeout → FAILED.

### AUTONOMOUS
- Agent running, processing orchestrator commands.
- No human attached.
- Structured channel active (relay ↔ wrapper ↔ extension).
- Events streaming to relay for observation.

### OBSERVING
- Human attached in read-only mode.
- Agent continues autonomous work uninterrupted.
- Human receives event stream via relay (or raw PTY output if local).
- Orchestrator retains full control.

### INJECTING
- Human can send messages into the agent's conversation.
- Agent processes human messages interleaved with orchestrator messages.
- **Conflict resolution**: human messages take priority; orchestrator is notified of human presence and can choose to pause or continue.

### TAKEOVER
- Human has full terminal control.
- **Orchestrator input is paused** (queued, not dropped).
- Raw PTY forwarded to human.
- Agent responds to human input only.
- Orchestrator receives a `session.takeover` event and can observe but not send.

### FAILED
- Agent crashed or became unreachable.
- Failure policy evaluated:
  - `restart`: transition back to STARTING (with history replay).
  - `notify`: stay in FAILED, alert orchestrator.
  - `checkpoint-retry`: restart from last checkpoint.
- Configurable per-task.

### STOPPED
- Agent exited cleanly (task complete, explicit kill, or shutdown).
- JSONL history preserved on host.
- Session metadata retained in registry.

## Transitions

| From | Event | To | Side Effects |
|------|-------|----|-------------|
| — | `spawn` | STARTING | Wrapper+pi-mono launched on target host |
| STARTING | `agent.ready` | AUTONOMOUS | Extension confirms connection; relay notified |
| STARTING | `timeout` / `error` | FAILED | Failure policy evaluated |
| AUTONOMOUS | `human.attach(observe)` | OBSERVING | Relay streams events to human client |
| AUTONOMOUS | `human.attach(inject)` | INJECTING | Human can send messages |
| AUTONOMOUS | `human.attach(takeover)` | TAKEOVER | Orchestrator prompts queue, permission authority moves to human, PTY byte forwarding is available |
| OBSERVING | `human.escalate(inject)` | INJECTING | — |
| OBSERVING | `human.detach` | AUTONOMOUS | — |
| INJECTING | `human.escalate(takeover)` | TAKEOVER | Orchestrator paused |
| INJECTING | `human.deescalate(observe)` | OBSERVING | — |
| INJECTING | `human.detach` | AUTONOMOUS | — |
| TAKEOVER | `human.deescalate(inject)` | INJECTING | Orchestrator resumed |
| TAKEOVER | `human.deescalate(observe)` | OBSERVING | Orchestrator resumed |
| TAKEOVER | `human.detach` | AUTONOMOUS | Orchestrator resumed, queued messages delivered |
| AUTONOMOUS | `task.complete` / `kill` | STOPPED | Cleanup, history preserved |
| AUTONOMOUS | `agent.crash` / `timeout` | FAILED | Failure policy evaluated |
| OBSERVING/INJECTING/TAKEOVER | `agent.crash` | FAILED | Human disconnected, failure policy evaluated |
| FAILED | `retry` | STARTING | History replayed into new instance |

## Invariants

1. At most **one human** attached to a session at any time.
2. In TAKEOVER, orchestrator **cannot send commands** (queued until de-escalation or detach).
3. State transitions are **atomic** and broadcast to all interested parties (relay, orchestrator, attached human).
4. FAILED → STARTING retry respects a **max retry count** per task.
5. STOPPED is **terminal** — a stopped session cannot be restarted (spawn a new one instead).
