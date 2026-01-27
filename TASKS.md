# Agentic Weave

This file tracks current work items and progress.

## Current State

**Status:** Phase 3C implemented locally (capabilities rename + Anthropic provider + e2e). PR #6 open for review.

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

**Phase 3C - Capabilities Rename + Anthropic Provider:**
- Renamed `adapters` package to `capabilities` with doc updates
- Added Anthropic provider (`agentic/providers/anthropic`)
- Added Anthropic e2e test mirroring Vertex flow
- Added Anthropic provider docs

### Next Steps
- [ ] Merge PR #6
- [ ] Tag release (if appropriate)
- [ ] Decide whether to ship the capabilities rename + Anthropic provider as a separate PR or fold into an existing one

### feat/agentic-weave-streaming*
- [x] Add Anthropic streaming API (text/tool/done/error events + stop reason normalization)
- [x] Default tool schema to `{type:"object",properties:{}}` when absent
- [x] Normalize tool result content for tool_result blocks
- [x] Add stream unit tests + streaming e2e coverage
- [x] Update Anthropic provider docs + streaming example

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
- **Capabilities vs adapters** - Capabilities provide feature flags; provider packages own message conversion.

### PR #6 Commits
```
refactor(budget): replace Message type with Budgetable interface
refactor(loop): extract recordAssistantMessage helper
docs: update TASKS.md with Phase 3B completion
+ earlier Phase 3A commits (AgentMessage, legacy cleanup, tests)
```

---

## Completed Initiatives

### phase-3c-capabilities-anthropic-provider ✅
Rename adapters to capabilities and add Anthropic provider + e2e coverage.

- [x] Rename `adapters` package to `capabilities` across code and docs
- [x] Add `agentic/providers/anthropic` provider (SDK-based)
- [x] Add Anthropic e2e test (tool-call roundtrip)
- [x] Document Anthropic provider usage

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

_Note: In the current codebase, conversion helpers live in provider packages; capabilities expose flags only._

**File:** `capabilities/vertex/vertex.go`

```go
func convertToVertexFormat(messages []message.AgentMessage) []vertexContent {
    // Convert structured tool calls to Vertex function call format
    // No text flattening - preserve structure
}
```

**File:** `capabilities/anthropic/anthropic.go`

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

## Progress Log
- 2026-01-27 21:10: Added Anthropic streaming support (stream API, tests, docs, e2e update, example) + schema/tool-result normalization.
- 2026-01-26: Renamed adapters to capabilities; added Anthropic provider, e2e test, and docs.
- 2026-01-26: Implemented loop history persistence, tool call normalization, Vertex text+tool call preservation, and paired compaction events with tests.
- 2026-01-26: Added follow-up fixes list for loop/history/Vertex/event pairing.
- 2026-01-26: Removed Conductor app task definitions from TASKS.md (external service).
- 2026-01-25: Phase 3B complete: loop refactoring, Budgetable interface, tool error counting.
- 2026-01-24: Updated docs/06-context-budgets.md for AgentMessage architecture.
- 2026-01-24: Completed Phase 3A cleanup: tests, refactoring, docs. PR #6 created.
- 2026-01-24: Completed Phase 3A message architecture refactoring.
- 2026-01-18: Added toolscope, tool history persistence, tests, and docs.
- 2026-01-17: Applied gofmt, removed unused test helper for staticcheck.
- 2026-01-16: Added Vertex Gemini provider, CI workflow, harness tests.
- 2026-01-14: Initial scaffolding, core module, docs, examples, LICENSE.
