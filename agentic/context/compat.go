package context

import (
	stdctx "context"

	"github.com/victorarias/agentic-weave/agentic/context/budget"
)

// ToBudgetMessages converts context messages to budget messages.
func ToBudgetMessages(messages []Message) []budget.Message {
	out := make([]budget.Message, len(messages))
	for i, msg := range messages {
		out[i] = budget.Message{Role: msg.Role, Content: msg.Content}
	}
	return out
}

// FromBudgetMessages converts budget messages to context messages.
func FromBudgetMessages(messages []budget.Message) []Message {
	out := make([]Message, len(messages))
	for i, msg := range messages {
		out[i] = Message{Role: msg.Role, Content: msg.Content}
	}
	return out
}

// BudgetPolicyFromLegacy creates a budget policy from legacy settings.
func BudgetPolicyFromLegacy(m Manager) budget.Policy {
	return budget.Policy{
		ContextWindow: m.MaxTokens,
		KeepLast:      m.KeepLast,
	}
}

// ToBudget adapts a legacy Manager to the budget.Manager API.
func (m Manager) ToBudget(policy budget.Policy) budget.Manager {
	if policy.ContextWindow == 0 {
		policy.ContextWindow = m.MaxTokens
	}
	if policy.KeepLast == 0 {
		policy.KeepLast = m.KeepLast
	}
	var compactor budget.Compactor
	if m.CompactFunc != nil {
		compactor = compactorFunc{fn: m.CompactFunc}
	}
	return budget.Manager{
		Counter:   m.Counter,
		Compactor: compactor,
		Policy:    policy,
	}
}

type compactorFunc struct {
	fn CompactionFunc
}

func (c compactorFunc) Compact(ctx stdctx.Context, messages []budget.Message) (string, error) {
	if c.fn == nil {
		return "", nil
	}
	legacy := FromBudgetMessages(messages)
	return c.fn(ctx, legacy)
}
