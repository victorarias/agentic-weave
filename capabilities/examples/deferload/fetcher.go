package deferload

import (
	"context"
	"fmt"
	"sync"

	"github.com/victorarias/agentic-weave/agentic"
)

// MapFetcher lazily returns tools from a map.
type MapFetcher struct {
	mu    sync.RWMutex
	tools map[string]agentic.Tool
}

func NewMapFetcher() *MapFetcher {
	return &MapFetcher{tools: make(map[string]agentic.Tool)}
}

func (m *MapFetcher) Register(tool agentic.Tool) {
	if tool == nil {
		return
	}
	def := tool.Definition()
	if def.Name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools[def.Name] = tool
}

func (m *MapFetcher) FetchTool(ctx context.Context, name string) (agentic.Tool, error) {
	m.mu.RLock()
	tool := m.tools[name]
	m.mu.RUnlock()
	if tool == nil {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return tool, nil
}
