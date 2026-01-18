package budget

import "context"

// Message represents a minimal conversational message for budgeting.
type Message struct {
	Role    string
	Content string
}

// TokenCounter estimates token usage.
type TokenCounter interface {
	Count(text string) int
}

// Compactor produces a summary for messages that will be compacted away.
type Compactor interface {
	Compact(ctx context.Context, messages []Message) (string, error)
}

// Policy configures compaction thresholds.
type Policy struct {
	ContextWindow    int
	ReserveTokens    int
	KeepRecentTokens int
	KeepLast         int
}

// Manager handles context compaction when token budgets are exceeded.
type Manager struct {
	Counter   TokenCounter
	Compactor Compactor
	Policy    Policy
}

// CharCounter estimates tokens by characters per token (default 4).
type CharCounter struct {
	CharsPerToken int
}

// Count implements TokenCounter.
func (c CharCounter) Count(text string) int {
	per := c.CharsPerToken
	if per <= 0 {
		per = 4
	}
	if text == "" {
		return 0
	}
	return (len(text) + per - 1) / per
}

// EstimateTokens sums token estimates for the message list.
func EstimateTokens(messages []Message, counter TokenCounter) int {
	if counter == nil {
		return 0
	}
	total := 0
	for _, msg := range messages {
		total += counter.Count(msg.Content)
	}
	return total
}

// CompactIfNeeded compacts messages when budget thresholds are exceeded.
func (m Manager) CompactIfNeeded(ctx context.Context, messages []Message) ([]Message, string, bool, error) {
	if m.Counter == nil || m.Compactor == nil || m.Policy.ContextWindow <= 0 {
		return messages, "", false, nil
	}

	total := EstimateTokens(messages, m.Counter)
	threshold := m.Policy.ContextWindow - max(m.Policy.ReserveTokens, 0)
	if total <= threshold {
		return messages, "", false, nil
	}

	start := m.cutPoint(messages)
	if start <= 0 || start >= len(messages) {
		return messages, "", false, nil
	}

	toCompact := messages[:start]
	if len(toCompact) == 0 {
		return messages, "", false, nil
	}

	summary, err := m.Compactor.Compact(ctx, toCompact)
	if err != nil {
		return messages, "", false, err
	}

	compacted := make([]Message, 0, len(messages)-len(toCompact)+1)
	compacted = append(compacted, Message{Role: "system", Content: summary})
	compacted = append(compacted, messages[start:]...)
	return compacted, summary, true, nil
}

func (m Manager) cutPoint(messages []Message) int {
	keepTokens := m.Policy.KeepRecentTokens
	if keepTokens > 0 && m.Counter != nil {
		acc := 0
		for i := len(messages) - 1; i >= 0; i-- {
			acc += m.Counter.Count(messages[i].Content)
			if acc >= keepTokens {
				return i
			}
		}
		return 0
	}

	keepLast := m.Policy.KeepLast
	if keepLast <= 0 {
		return 0
	}
	start := len(messages) - keepLast
	if start < 0 {
		start = 0
	}
	return start
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
