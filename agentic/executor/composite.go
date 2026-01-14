package executor

import (
	"context"
	"errors"
	"sort"

	"github.com/victorarias/agentic-weave/agentic"
)

// Composite routes tool calls across multiple executors.
type Composite struct {
	executors []agentic.ToolExecutor
}

func NewComposite(executors ...agentic.ToolExecutor) *Composite {
	return &Composite{executors: executors}
}

func (c *Composite) ListTools(ctx context.Context) ([]agentic.ToolDefinition, error) {
	seen := make(map[string]agentic.ToolDefinition)
	for _, exec := range c.executors {
		defs, err := exec.ListTools(ctx)
		if err != nil {
			return nil, err
		}
		for _, def := range defs {
			if _, ok := seen[def.Name]; !ok {
				seen[def.Name] = def
			}
		}
	}
	list := make([]agentic.ToolDefinition, 0, len(seen))
	for _, def := range seen {
		list = append(list, def)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list, nil
}

func (c *Composite) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	for _, exec := range c.executors {
		result, err := exec.Execute(ctx, call)
		if err == nil {
			return result, nil
		}
		if errors.Is(err, agentic.ErrToolNotFound) {
			continue
		}
		return result, err
	}
	return agentic.ToolResult{}, agentic.ErrToolNotFound
}
