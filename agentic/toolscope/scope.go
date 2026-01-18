package toolscope

import "context"

// ToolScope carries execution metadata for agent tools.
type ToolScope struct {
	UserID         int64
	ConversationID string
	Platform       string
}

type scopeKey struct{}

// WithScope attaches a tool scope to the context.
func WithScope(ctx context.Context, scope ToolScope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// ScopeFromContext extracts the tool scope from context.
func ScopeFromContext(ctx context.Context) (ToolScope, bool) {
	scope, ok := ctx.Value(scopeKey{}).(ToolScope)
	return scope, ok
}
