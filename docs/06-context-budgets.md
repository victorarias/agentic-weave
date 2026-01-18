# Context Budgets & Compaction (Design)

This document proposes optional, pluggable context budgeting + compaction + truncation modules that bring mono-like usability to Agentic Weave while keeping the core LLM-agnostic and minimal.

## Goals
- Optional modules only; core remains unchanged and LLM-agnostic.
- Seamless usability: a few lines of setup to get safe context budgets, compaction, and tool output truncation.
- Pluggable policy: allow custom token counters, compaction strategies, and provider adapters.
- Clear, testable interfaces with minimal surface area.

## Non-goals
- Hard-coding provider SDKs or model-specific logic in core.
- Forcing a single agent loop or storage layer.
- Perfect token accounting (estimation is acceptable with overrides).

## Design Principles
- Additive, backwards-compatible changes.
- Optional modules live in subpackages and can be imported independently.
- Interfaces are small and composable.

---

## Proposed Modules (Optional)

### 1) `agentic/limits`
Lightweight model metadata and limit helpers.

```go
package limits

type ModelLimits struct {
	ContextWindow int // total context tokens
	MaxOutput     int // max completion tokens
}

// Optional provider for model-specific limits.
type Provider interface {
	Limits() ModelLimits
}
```

Use: adapters can expose limits; callers can inject constants.

### 2) `agentic/usage`
Standardize usage/stop-reason reporting without locking to a provider.

```go
package usage

type Usage struct {
	Input  int
	Output int
	Total  int
}

type StopReason string

const (
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonError     StopReason = "error"
	StopReasonAbort     StopReason = "abort"
)
```

Adapters can map provider responses to this shape. This keeps context budget logic decoupled from any SDK.

### 3) `agentic/truncate`
Shared truncation helpers for tool outputs, with predictable guarantees.

```go
package truncate

type Options struct {
	MaxLines int
	MaxBytes int
}

type Result struct {
	Content     string
	Truncated   bool
	TruncatedBy string // "lines" | "bytes"
	TotalLines  int
	TotalBytes  int
}

func Head(content string, opts Options) Result
func Tail(content string, opts Options) Result
```

Provide a thin wrapper to apply truncation to `ToolResult.Output` before it enters history.

### 4) `agentic/context/budget`
Context management that optionally compacts history based on token budgets.

```go
package budget

import "context"

// Minimal message representation for compaction.
type Message struct {
	Role    string
	Content string
}

// TokenCounter estimates token usage. Can be heuristic or provider-specific.
type TokenCounter interface {
	Count(text string) int
}

// Compactor turns messages into a summary.
type Compactor interface {
	Compact(ctx context.Context, messages []Message) (string, error)
}

// Policy configures thresholds.
type Policy struct {
	ContextWindow    int
	ReserveTokens    int
	KeepRecentTokens int
	KeepLast         int
}

// Manager evaluates when to compact and applies compaction if needed.
type Manager struct {
	Counter   TokenCounter
	Compactor Compactor
	Policy    Policy
}

func (m Manager) CompactIfNeeded(ctx context.Context, messages []Message) (out []Message, summary string, changed bool, err error)
```

Notes:
- `KeepRecentTokens` keeps a token budget of recent messages; `KeepLast` is a simpler fallback (count of messages).
- If `Counter` or `Compactor` is nil, it becomes a no-op (fully optional).
- This package can wrap or supersede the existing `agentic/context.Manager`, or be added alongside it as `context/budget` for clarity.

### 5) `agentic/events` (Optional Integration)
Use the existing `events` module to emit compaction and truncation events. No core dependency.

```go
// Example event types (stringly-typed):
// "context_compaction_start", "context_compaction_end", "tool_output_truncated"
```

---

## Agent Loop Integration (Optional, Seamless)
Provide a reference helper in a new optional package `agentic/loop` (or similar) that wires:
- history management
- budget compaction
- tool result truncation
- model usage reporting (when available)

### Minimal interface (example)
```go
package loop

type Decider interface {
	Decide(ctx context.Context, in Input) (Decision, error)
}

type Input struct {
	SystemPrompt string
	UserMessage  string
	History      []budget.Message
	Tools        []agentic.ToolDefinition
	ToolCalls    []agentic.ToolCall
	ToolResults  []agentic.ToolResult
	Turn         int
}

type Decision struct {
	Reply      string
	ToolCalls  []agentic.ToolCall
	Usage      *usage.Usage
	StopReason usage.StopReason
}
```

The loop helper remains optional, but makes “mono-like” integration a few lines:

```go
mgr := budget.Manager{
	Counter: myCounter,
	Compactor: myCompactor,
	Policy: budget.Policy{ContextWindow: 200000, ReserveTokens: 16000, KeepRecentTokens: 20000},
}

trunc := truncate.Options{MaxLines: 2000, MaxBytes: 50 * 1024}

agent := loop.New(loop.Config{
	Decider:        myDecider,
	Executor:       registry,
	Budget:         &mgr,
	Truncation:     &trunc,
	TruncationMode: truncate.ModeTail,
})
```

If your provider requires full tool-use history, implement `history.ToolRecorder` and `history.ToolLoader` on
your `HistoryStore` to persist tool calls and results between turns.

---

## Context Budgeting Behavior
1) Count total tokens from messages.
2) If total exceeds `ContextWindow - ReserveTokens`, compact older messages:
   - Summarize older messages using `Compactor`.
   - Keep recent messages based on `KeepRecentTokens` (or `KeepLast` fallback).
3) Insert summary as a system message at the front.

This mirrors mono’s “reserve + keep recent + summary” model, but remains pluggable.

---

## Provider Adapters
Adapters can (optionally) expose:
- `limits.ModelLimits` for context + max output
- `usage.Usage` + `usage.StopReason`
- Finish-reason mapping (e.g., `max_tokens`)

This lets the loop detect truncation and decide whether to compact or warn the user.

---

## Storage & Persistence (Optional Hook)
Define a minimal interface to allow saving compaction summaries and history if needed, but do not mandate a store.
When using the loop runner with compaction enabled, the history store must also implement `history.Rewriter` so compaction can replace old messages.

```go
package history

type Store interface {
	Append(ctx context.Context, msg budget.Message) error
	Load(ctx context.Context) ([]budget.Message, error)
}
```

---

## Backwards Compatibility
- All changes are additive and optional.
- Existing `agentic/context.Manager` remains intact. New budget manager can live alongside or evolve it (non-breaking).
- No existing examples are forced to change.

---

## Documentation & Examples
- Add this design doc to the docs index.
- See `examples/mono-like` for a one-file setup with compaction + truncation.

---

## Open Questions
- Should `context/budget` replace `context.Manager` or exist in parallel?
- Default token counter implementation (chars/4 heuristic vs. provider-specific)?
- Should `KeepRecentTokens` default to a ratio of `ContextWindow`?
- How should tool output truncation be represented in the message history (e.g., with a marker)?

---

## Phased Implementation Outline
1) Add `limits`, `usage`, `truncate`, and `context/budget` packages (all optional).
2) Add compaction + truncation tests.
3) Add `loop` package with reference implementation.
4) Document adapters + example usage.
