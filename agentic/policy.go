package agentic

import "fmt"

// Policy guards tool visibility and execution.
type Policy interface {
	AllowTool(def ToolDefinition) error
	AllowCall(def ToolDefinition, call ToolCall) error
}

// AllowAllPolicy is the default permissive policy.
type AllowAllPolicy struct{}

func (AllowAllPolicy) AllowTool(ToolDefinition) error           { return nil }
func (AllowAllPolicy) AllowCall(ToolDefinition, ToolCall) error { return nil }

// AllowlistPolicy permits only explicit tool names.
type AllowlistPolicy struct {
	allowed map[string]struct{}
}

func NewAllowlistPolicy(names []string) AllowlistPolicy {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	return AllowlistPolicy{allowed: allowed}
}

func (p AllowlistPolicy) AllowTool(def ToolDefinition) error {
	if _, ok := p.allowed[def.Name]; !ok {
		return fmt.Errorf("tool not allowlisted: %s", def.Name)
	}
	return nil
}

func (p AllowlistPolicy) AllowCall(def ToolDefinition, call ToolCall) error {
	return p.AllowTool(def)
}
