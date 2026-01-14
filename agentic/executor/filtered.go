package executor

import (
	"context"

	"github.com/victorarias/agentic-weave/agentic"
)

// FilteredExecutor hides tools not in the allowlist.
type FilteredExecutor struct {
	inner   agentic.ToolExecutor
	allowed map[string]struct{}
}

func NewFiltered(inner agentic.ToolExecutor, allowlist []string) *FilteredExecutor {
	allowed := make(map[string]struct{}, len(allowlist))
	for _, name := range allowlist {
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	return &FilteredExecutor{inner: inner, allowed: allowed}
}

func (f *FilteredExecutor) ListTools(ctx context.Context) ([]agentic.ToolDefinition, error) {
	defs, err := f.inner.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]agentic.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		if _, ok := f.allowed[def.Name]; ok {
			filtered = append(filtered, def)
		}
	}
	return filtered, nil
}

func (f *FilteredExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if _, ok := f.allowed[call.Name]; !ok {
		return agentic.ToolResult{}, agentic.ErrToolNotFound
	}
	return f.inner.Execute(ctx, call)
}
