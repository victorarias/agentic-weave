package budget

import "context"

// Budgetable represents a message that can be counted for token budgeting.
type Budgetable interface {
	BudgetRole() string
	BudgetContent() string
}

// TokenCounter estimates token usage.
type TokenCounter interface {
	Count(text string) int
}

// Compactor produces a summary for messages that will be compacted away.
type Compactor interface {
	Compact(ctx context.Context, messages []Budgetable) (string, error)
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
func EstimateTokens[T Budgetable](messages []T, counter TokenCounter) int {
	if counter == nil {
		return 0
	}
	total := 0
	for _, msg := range messages {
		total += counter.Count(msg.BudgetContent())
	}
	return total
}

// CompactIfNeeded compacts messages when budget thresholds are exceeded.
// Returns the summary, number of messages to keep from the end, whether compaction occurred, and any error.
func (m Manager) CompactIfNeeded(ctx context.Context, messages []Budgetable) (summary string, keepCount int, changed bool, err error) {
	if m.Counter == nil || m.Compactor == nil || m.Policy.ContextWindow <= 0 {
		return "", len(messages), false, nil
	}

	total := EstimateTokens(messages, m.Counter)
	threshold := m.Policy.ContextWindow - max(m.Policy.ReserveTokens, 0)
	if total <= threshold {
		return "", len(messages), false, nil
	}

	start := m.cutPoint(messages)
	if start <= 0 || start >= len(messages) {
		return "", len(messages), false, nil
	}

	toCompact := messages[:start]
	if len(toCompact) == 0 {
		return "", len(messages), false, nil
	}

	summary, err = m.Compactor.Compact(ctx, toCompact)
	if err != nil {
		return "", len(messages), false, err
	}

	keepCount = len(messages) - start
	return summary, keepCount, true, nil
}

func (m Manager) cutPoint(messages []Budgetable) int {
	keepTokens := m.Policy.KeepRecentTokens
	if keepTokens > 0 && m.Counter != nil {
		acc := 0
		for i := len(messages) - 1; i >= 0; i-- {
			acc += m.Counter.Count(messages[i].BudgetContent())
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
