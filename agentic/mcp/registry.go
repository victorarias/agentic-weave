package mcp

import (
	"context"

	"github.com/victorarias/agentic-weave/agentic"
)

// Client bridges MCP tool execution.
type Client interface {
	ListTools(ctx context.Context) ([]agentic.ToolDefinition, error)
	Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
}

// Registry exposes MCP tools with allowlist gating.
type Registry struct {
	client    Client
	allowlist map[string]struct{}
}

func NewRegistry(client Client, allowlist []string) *Registry {
	allowed := make(map[string]struct{}, len(allowlist))
	for _, name := range allowlist {
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	return &Registry{client: client, allowlist: allowed}
}

func (r *Registry) ListTools(ctx context.Context) ([]agentic.ToolDefinition, error) {
	if r.client == nil {
		return nil, nil
	}
	defs, err := r.client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	if len(r.allowlist) == 0 {
		return defs, nil
	}
	filtered := make([]agentic.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		if _, ok := r.allowlist[def.Name]; ok {
			filtered = append(filtered, def)
		}
	}
	return filtered, nil
}

func (r *Registry) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if r.client == nil {
		return agentic.ToolResult{}, agentic.ErrToolNotFound
	}
	if len(r.allowlist) > 0 {
		if _, ok := r.allowlist[call.Name]; !ok {
			return agentic.ToolResult{}, agentic.ErrToolNotFound
		}
	}
	return r.client.Execute(ctx, call)
}
