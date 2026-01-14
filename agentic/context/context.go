package context

import "context"

// Message represents a minimal conversational message.
type Message struct {
	Role    string
	Content string
}

// TokenCounter estimates token usage.
type TokenCounter interface {
	Count(text string) int
}

// CompactionFunc compresses messages into a summary.
type CompactionFunc func(ctx context.Context, messages []Message) (string, error)

// Manager handles compaction based on token limits.
type Manager struct {
	Counter      TokenCounter
	MaxTokens    int
	KeepLast     int
	CompactFunc  CompactionFunc
}

// CompactIfNeeded returns compacted messages and summary (if any).
func (m Manager) CompactIfNeeded(ctx context.Context, messages []Message) ([]Message, string, error) {
	if m.Counter == nil || m.MaxTokens <= 0 || m.CompactFunc == nil {
		return messages, "", nil
	}
	total := 0
	for _, msg := range messages {
		total += m.Counter.Count(msg.Content)
	}
	if total <= m.MaxTokens {
		return messages, "", nil
	}

	keep := m.KeepLast
	if keep < 0 {
		keep = 0
	}
	if keep > len(messages) {
		keep = len(messages)
	}
	start := len(messages) - keep
	if start < 0 {
		start = 0
	}
	toCompact := messages[:start]
	summary, err := m.CompactFunc(ctx, toCompact)
	if err != nil {
		return messages, "", err
	}

	compacted := make([]Message, 0, len(messages)-len(toCompact)+1)
	compacted = append(compacted, Message{Role: "system", Content: summary})
	compacted = append(compacted, messages[start:]...)
	return compacted, summary, nil
}
