package history

import (
	"context"

	"github.com/victorarias/agentic-weave/agentic/context/budget"
)

// Store persists conversational messages.
type Store interface {
	Append(ctx context.Context, msg budget.Message) error
	Load(ctx context.Context) ([]budget.Message, error)
}

// Rewriter can replace stored messages (used after compaction).
type Rewriter interface {
	Store
	Replace(ctx context.Context, messages []budget.Message) error
}

// MemoryStore stores messages in memory.
type MemoryStore struct {
	messages []budget.Message
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

// Replace overwrites stored messages.
func (m *MemoryStore) Replace(ctx context.Context, messages []budget.Message) error {
	m.messages = make([]budget.Message, len(messages))
	copy(m.messages, messages)
	return nil
}
