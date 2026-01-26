package history

import (
	"context"

	"github.com/victorarias/agentic-weave/agentic/message"
)

// Store persists conversational messages.
type Store interface {
	Append(ctx context.Context, msg message.AgentMessage) error
	Load(ctx context.Context) ([]message.AgentMessage, error)
}

// Rewriter can replace stored messages (used after compaction).
type Rewriter interface {
	Store
	Replace(ctx context.Context, messages []message.AgentMessage) error
}

// MemoryStore stores messages in memory.
type MemoryStore struct {
	messages []message.AgentMessage
}

// NewMemoryStore creates an in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Append stores a message.
func (m *MemoryStore) Append(ctx context.Context, msg message.AgentMessage) error {
	m.messages = append(m.messages, msg)
	return nil
}

// Load returns stored messages.
func (m *MemoryStore) Load(ctx context.Context) ([]message.AgentMessage, error) {
	out := make([]message.AgentMessage, len(m.messages))
	copy(out, m.messages)
	return out, nil
}

// Replace overwrites stored messages.
func (m *MemoryStore) Replace(ctx context.Context, messages []message.AgentMessage) error {
	m.messages = make([]message.AgentMessage, len(messages))
	copy(m.messages, messages)
	return nil
}
