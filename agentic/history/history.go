package history

import (
	"context"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
)

// Store persists conversational messages.
type Store interface {
	Append(ctx context.Context, msg budget.Message) error
	Load(ctx context.Context) ([]budget.Message, error)
}

// ToolRecorder persists tool usage alongside conversational messages.
type ToolRecorder interface {
	AppendToolCall(ctx context.Context, call agentic.ToolCall) error
	AppendToolResult(ctx context.Context, result agentic.ToolResult) error
}

// ToolLoader loads stored tool usage for request reconstruction.
type ToolLoader interface {
	LoadToolCalls(ctx context.Context) ([]agentic.ToolCall, error)
	LoadToolResults(ctx context.Context) ([]agentic.ToolResult, error)
}

// Rewriter can replace stored messages (used after compaction).
type Rewriter interface {
	Store
	Replace(ctx context.Context, messages []budget.Message) error
}

// MemoryStore stores messages in memory.
type MemoryStore struct {
	messages    []budget.Message
	toolCalls   []agentic.ToolCall
	toolResults []agentic.ToolResult
}

// NewMemoryStore creates an in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Append stores a message.
func (m *MemoryStore) Append(ctx context.Context, msg budget.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

// Load returns stored messages.
func (m *MemoryStore) Load(ctx context.Context) ([]budget.Message, error) {
	out := make([]budget.Message, len(m.messages))
	copy(out, m.messages)
	return out, nil
}

// AppendToolCall stores a tool call.
func (m *MemoryStore) AppendToolCall(ctx context.Context, call agentic.ToolCall) error {
	m.toolCalls = append(m.toolCalls, call)
	return nil
}

// AppendToolResult stores a tool result.
func (m *MemoryStore) AppendToolResult(ctx context.Context, result agentic.ToolResult) error {
	m.toolResults = append(m.toolResults, result)
	return nil
}

// LoadToolCalls returns stored tool calls.
func (m *MemoryStore) LoadToolCalls(ctx context.Context) ([]agentic.ToolCall, error) {
	out := make([]agentic.ToolCall, len(m.toolCalls))
	copy(out, m.toolCalls)
	return out, nil
}

// LoadToolResults returns stored tool results.
func (m *MemoryStore) LoadToolResults(ctx context.Context) ([]agentic.ToolResult, error) {
	out := make([]agentic.ToolResult, len(m.toolResults))
	copy(out, m.toolResults)
	return out, nil
}

// Replace overwrites stored messages.
func (m *MemoryStore) Replace(ctx context.Context, messages []budget.Message) error {
	m.messages = make([]budget.Message, len(messages))
	copy(m.messages, messages)
	return nil
}
