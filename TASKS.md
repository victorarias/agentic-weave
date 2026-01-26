# Agentic Weave

This file tracks current work items and progress.

## Current State

**Status:** Phase 3B complete. PR #6 open for review.

**Branch:** `feat/phase-3a-message-cleanup`
**PR:** https://github.com/victorarias/agentic-weave/pull/6

### What Was Done

**Phase 3A - Message Architecture:**
- Created `AgentMessage` type with structured tool calls/results (no more text flattening)
- Updated entire codebase: loop, history, vertex provider, events, examples, tests
- Deleted legacy context files (context.go, compat.go, with_system.go + tests)
- Removed low-value tests, added 8 high-value edge case tests

**Phase 3B - Loop & Budget Refactoring:**
- Extracted `recordAssistantMessage()` helper - consolidates history storage + event emission
- Removed default reply fallback from loop - providers handle empty replies
- Removed reply trimming from loop - providers handle formatting
- Replaced `budget.Message` with `Budgetable` interface (pi-mono pattern)
- `AgentMessage` implements `Budgetable` directly - no conversion needed
- `BudgetContent()` now includes tool errors in token estimation
- Removed `ForBudget`, `ForBudgetSlice`, `FromBudget`, `FromBudgetSlice` functions
- Updated docs with new architecture

### Next Steps
- [ ] Merge PR #6
- [ ] Tag release (if appropriate)

### Key Files
- `agentic/message/message.go` - AgentMessage type, implements Budgetable
- `agentic/context/budget/budget.go` - Budgetable interface, Manager, compaction logic
- `agentic/loop/loop.go` - Main agent loop with recordAssistantMessage helper
- `agentic/providers/vertex/vertex.go` - Vertex Gemini provider
- `docs/06-context-budgets.md` - Architecture documentation

### Architecture Decisions
- **Budgetable interface** - Follows pi-mono: work directly with rich AgentMessage, no separate "budget message" type. Avoids conversion bugs.
- **No default reply in loop** - Providers (Vertex) handle empty replies. Loop passes through what it gets.
- **No trimming in loop** - Providers handle formatting. Loop is a thin orchestrator.
- **Tool errors in BudgetContent()** - Fixes bug where large error messages bypassed compaction.

### PR #6 Commits
```
refactor(budget): replace Message type with Budgetable interface
refactor(loop): extract recordAssistantMessage helper
docs: update TASKS.md with Phase 3B completion
+ earlier Phase 3A commits (AgentMessage, legacy cleanup, tests)
```

---

## Completed Initiatives

### phase-3b-loop-budget-refactoring ✅
Simplify loop and budget code, follow pi-mono pattern more closely.

- [x] Extract `recordAssistantMessage()` helper in loop.go
- [x] Remove default reply fallback (providers handle this)
- [x] Remove reply trimming (providers handle formatting)
- [x] Replace `budget.Message` with `Budgetable` interface
- [x] `AgentMessage` implements `Budgetable` directly
- [x] Include tool errors in `BudgetContent()` for accurate token estimation
- [x] Remove conversion functions (ForBudget, FromBudget, etc.)
- [x] Update all Compactor implementations to use `[]Budgetable`
- [x] Update docs/06-context-budgets.md

### phase-3a-message-architecture ✅
Adopt pi-mono pattern: rich internal message type with structured tool calls, adapter-level conversion.

- [x] Create AgentMessage type in `agentic/message/message.go`
- [x] Update loop types (Request, Result, Input) to use `[]message.AgentMessage`
- [x] Remove `toolResultMessage()` function - no more text flattening
- [x] Update history Store interface to use AgentMessage
- [x] Remove ToolRecorder/ToolLoader interfaces (tool data now embedded)
- [x] Update Vertex provider - replace HistoryTurn with AgentMessage
- [x] Add ToolCalls field to Event struct for MessageEnd events
- [x] Delete legacy files: context.go, compat.go, compat_test.go, with_system.go, with_system_test.go
- [x] Update examples/basic/main.go
- [x] Update tests/harness/*
- [x] Remove low-value tests (6 tests across message, history, vertex, harness)
- [x] Add edge case tests (8 new tests in loop_edge_cases_test.go)
- [x] Extract `emit()` helper in loop.go
- [x] Add `envTrimmed()` helper in vertex.go
- [x] Simplify compaction logic with `max()` builtin
- [x] Update docs/06-context-budgets.md for AgentMessage

### vertex-provider ✅
- [x] Add Vertex Gemini provider (ADC-only)
- [x] Add docs + example usage

### loop-truncation-fixes ✅
- [x] Return partial output when head truncation hits first-line byte limit
- [x] Preserve byte-based truncation metadata for tail truncation
- [x] Avoid appending compaction summaries for history stores without rewrite support

### loop-history-rewriter ✅
- [x] Require history.Rewriter when budget compaction is configured

### test-harness ✅
- [x] Add integration harness covering loop, truncation, and compaction flows
- [x] Expand harness coverage across loop behavior, tools, and policies
- [x] Add guard, event ordering, byte truncation, and usage passthrough tests
- [x] Add MCP integration and Vertex config tests

### ci-harness ✅
- [x] Add GitHub Actions workflow to run all tests
- [x] Add formatter and linter checks to CI workflow
- [x] Switch CI linter to staticcheck

### mono-parity-context ✅
- [x] Design optional, pluggable context budgeting + compaction + truncation modules
- [x] Define minimal interfaces for model limits + usage reporting
- [x] Implement budget + truncation packages with tests
- [x] Add loop helper, adapter utilities, history hook

---

## Task Definitions (from conductor-bot)

### Phase 3A: agentic-weave Message Architecture (pi-mono Pattern)

**Why:** Fixes Issue 1 ([tool_call] echo) by preserving tool call structure. Adopts proven pi-mono pattern.

**Approach:** Rich internal message type + adapter-level conversion. No backward compatibility, clean API.

#### Task 3A.1: Create AgentMessage Type

**New file:** `agentic/message/message.go`

```go
package message

// AgentMessage is the rich internal representation (pi-mono pattern)
type AgentMessage struct {
    Role        string           // "user", "assistant", "tool"
    Content     string           // text content
    ToolCalls   []ToolCall       // structured tool calls (not text!)
    ToolResults []ToolResult     // structured tool results (not text!)
    Timestamp   time.Time
}

// Standard roles
const (
    RoleUser      = "user"
    RoleAssistant = "assistant"
    RoleTool      = "tool"
)
```

#### Task 3A.2: Update Loop to Use AgentMessage

**File:** `agentic/loop/loop.go`

**Changes:**
- Replace `[]budget.Message` with `[]message.AgentMessage` for history
- When appending tool calls: `AgentMessage{Role: "assistant", ToolCalls: calls}`
- When appending tool results: `AgentMessage{Role: "tool", ToolResults: results}`
- Remove text flattening helpers (`toolResultMessage()` etc.)

#### Task 3A.3: Add Message Events

**File:** `agentic/events/events.go`

**Changes:**
- `MessageEnd` - always emitted when LLM response complete (has full content)
- `MessageStart` - only emitted by streaming providers (signals LLM call started)
- `MessageUpdate` - only emitted by streaming providers (delta content)

```go
// MessageEnd is always emitted, even for non-streaming
emit.Emit(events.Event{
    Type:      events.MessageEnd,
    MessageID: msgID,
    Role:      "assistant",
    Content:   decision.Reply,
    ToolCalls: decision.ToolCalls,
})
```

#### Task 3A.4: Add Adapter Conversion Functions

Each adapter converts `[]AgentMessage` to provider-specific format.

**File:** `adapters/vertex/vertex.go`

```go
func convertToVertexFormat(messages []message.AgentMessage) []vertexContent {
    // Convert structured tool calls to Vertex function call format
    // No text flattening - preserve structure
}
```

**File:** `adapters/anthropic/anthropic.go`

```go
func convertToAnthropicFormat(messages []message.AgentMessage) []anthropicMessage {
    // Convert to Anthropic format with tool_use blocks
}
```

#### Task 3A.5: Keep budget.Message Minimal

**File:** `agentic/context/budget/budget.go`

Keep `budget.Message` for token counting only:
```go
type Message struct {
    Role    string
    Content string
    // NO tool calls - budget just needs text for token estimation
}
```

Add conversion helper:
```go
func FromAgentMessages(msgs []message.AgentMessage) []Message {
    // Flatten for token counting only
}
```

---

### Phase 3B: Reply Tool Architecture (conductor-bot)

**Why:** Consistent Slack output via tools. Enables multi-message responses. Fallback for safety.

#### Task 3B.1: Create reply_to_user Tool

**New file:** `internal/tools/reply_to_user.go`

Context-aware tool for conversation replies:

```go
type ReplyToUserTool struct {
    slack    ports.SlackPort
    channel  string  // injected per-activation
    threadTS string  // injected per-activation
}

func (t *ReplyToUserTool) Name() string { return "reply_to_user" }

func (t *ReplyToUserTool) Description() string {
    return "Send a message to the user. Write in your natural voice as Conductor."
}

func (t *ReplyToUserTool) Parameters() map[string]ParameterSpec {
    return map[string]ParameterSpec{
        "message": {Type: "string", Description: "The message to send", Required: true},
    }
}

// WithContext creates a copy with channel/thread for this activation
func (t *ReplyToUserTool) WithContext(channel, threadTS string) *ReplyToUserTool
```

**Keep post_to_slack** for explicit channel posting (standup briefs, announcements).

#### Task 3B.2: Wire Reply Tool in ConductorFlow

**File:** `internal/commands/conductor_flow.go`

- Add `replyTool` field
- In `Run()`: create context-aware copy with `req.ChannelID`/`req.ThreadTS`
- Add to tools list for this activation

**File:** `cmd/conductor/main.go`

- Create base tool, pass to ConductorFlow

#### Task 3B.3: Add Fallback Auto-Post (clawdbot pattern)

**File:** `internal/server/socketmode.go`

If LLM returns text in `result.Reply` but didn't call `reply_to_user`, auto-post it as fallback:

```go
func (s *SocketModeServer) runMentionFlow(ctx context.Context, req commands.CommandRequest) error {
    result, err := s.mentionFlow.Run(ctx, req)
    if err != nil {
        return err
    }

    payload := result.Payload.(commands.ConductorResult)

    // Check if reply_to_user was called (track via tool results or flag)
    if !payload.RepliedViaTool && payload.Reply != "" {
        // Fallback: auto-post if LLM forgot to use the tool
        log.Warn().Str("activation_id", req.ActivationID).
            Msg("LLM returned text without calling reply_to_user, auto-posting as fallback")
        s.slack.PostMessageInThread(ctx, req.ChannelID, payload.Reply, req.ThreadTS)
    }
    return nil
}
```

#### Task 3B.4: Remove assistant_reply Storage

**File:** `internal/commands/session.go`

- Remove `AssistantReply` field from `ConversationTurn` and `BrainHistoryTurn`

**File:** `internal/session/migrations.go`

- Add migration v4 to drop `assistant_reply` column

**File:** `internal/session/sqlite_store.go`

- Remove `assistant_reply` from SELECT/INSERT queries

**File:** `internal/commands/conductor_flow.go`

- Update `saveTurn()` - no reply parameter
- Remove `formatToolCall()`, `historyToBudgetMessages()` text flattening
- Update to use new `[]message.AgentMessage` from agentic-weave

---

### Phase 3C: Increase Max Turns

**Why:** Allow more tool calls per conversation turn.

#### Task 3C.1: Make max turns configurable

**File:** `internal/commands/conductor_flow.go:66`

```go
func NewConductorFlow(...) *ConductorFlow {
    maxTurns := 10
    if v := os.Getenv("CONDUCTOR_MAX_TURNS"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 {
            maxTurns = n
        }
    }
    // ...
}
```

---

## Progress Log
- 2026-01-25: Phase 3B complete: loop refactoring, Budgetable interface, tool error counting.
- 2026-01-24: Updated docs/06-context-budgets.md for AgentMessage architecture.
- 2026-01-24: Completed Phase 3A cleanup: tests, refactoring, docs. PR #6 created.
- 2026-01-24: Completed Phase 3A message architecture refactoring.
- 2026-01-18: Added toolscope, tool history persistence, tests, and docs.
- 2026-01-17: Applied gofmt, removed unused test helper for staticcheck.
- 2026-01-16: Added Vertex Gemini provider, CI workflow, harness tests.
- 2026-01-14: Initial scaffolding, core module, docs, examples, LICENSE.
