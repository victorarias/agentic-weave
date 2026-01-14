package agentic

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Registry stores tool implementations and executes calls.
type Registry struct {
	mu     sync.RWMutex
	tools  map[string]Tool
	policy Policy
}

// RegistryOption configures a Registry.
type RegistryOption func(*Registry)

// WithPolicy sets the registry policy.
func WithPolicy(policy Policy) RegistryOption {
	return func(r *Registry) {
		if policy != nil {
			r.policy = policy
		}
	}
}

// NewRegistry creates an empty registry with optional policy.
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		tools:  make(map[string]Tool),
		policy: AllowAllPolicy{},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register adds tools to the registry.
func (r *Registry) Register(tools ...Tool) error {
	for i, tool := range tools {
		if tool == nil {
			return fmt.Errorf("tool at index %d is nil", i)
		}
		def := tool.Definition()
		if def.Name == "" {
			return fmt.Errorf("tool at index %d has empty name", i)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tool := range tools {
		def := tool.Definition()
		r.tools[def.Name] = tool
	}
	return nil
}

// ListTools returns tool definitions in stable order.
func (r *Registry) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		def := tool.Definition()
		if err := r.policy.AllowTool(def); err != nil {
			continue
		}
		defs = append(defs, def)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})

	return defs, nil
}

// Execute routes a tool call to the registered tool.
func (r *Registry) Execute(ctx context.Context, call ToolCall) (ToolResult, error) {
	r.mu.RLock()
	tool := r.tools[call.Name]
	r.mu.RUnlock()
	if tool == nil {
		return ToolResult{}, ErrToolNotFound
	}

	def := tool.Definition()
	if err := r.policy.AllowCall(def, call); err != nil {
		return ToolResult{}, err
	}
	if err := validateCaller(def, call); err != nil {
		return ToolResult{}, err
	}
	if def.SchemaHash != "" && call.SchemaHash != "" && def.SchemaHash != call.SchemaHash {
		return ToolResult{}, ErrSchemaMismatch
	}

	select {
	case <-ctx.Done():
		return ToolResult{}, ctx.Err()
	default:
	}

	result, err := tool.Execute(ctx, call)
	if err != nil {
		return result, err
	}
	return result, nil
}

func validateCaller(def ToolDefinition, call ToolCall) error {
	if len(def.AllowedCallers) == 0 {
		return nil
	}
	if call.Caller == nil || call.Caller.Type == "" {
		return ErrCallerNotAllowed
	}
	for _, allowed := range def.AllowedCallers {
		if allowed == call.Caller.Type {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrCallerNotAllowed, call.Caller.Type)
}
