package tools

import (
	"context"
	"os"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
)

type lsInput struct {
	Path       string `json:"path,omitempty"`
	All        bool   `json:"all,omitempty"`
	MaxEntries int    `json:"max_entries,omitempty"`
}

// LSTool lists directory entries.
type LSTool struct {
	WorkDir string
}

func (t LSTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        "ls",
		Description: "List directory contents.",
		InputSchema: toJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string", "description": "Directory path (default current workspace)."},
				"all":         map[string]any{"type": "boolean", "description": "Include hidden files."},
				"max_entries": map[string]any{"type": "integer", "description": "Maximum number of returned entries."},
			},
		}),
	}
}

func (t LSTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	var input lsInput
	if res := parseInput(call, &input); res != nil {
		return *res, nil
	}
	path := input.Path
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	resolved, err := resolvePath(t.WorkDir, path)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	limit := sanitizeLimit(input.MaxEntries, 500, 5000)
	type item struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Size int64  `json:"size"`
		Mode string `json:"mode"`
	}
	items := make([]item, 0, minInt(len(entries), limit))
	truncated := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
		}
		if !input.All && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if len(items) >= limit {
			truncated = true
			break
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		if info.Mode()&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		items = append(items, item{Name: entry.Name(), Type: kind, Size: info.Size(), Mode: info.Mode().String()})
	}

	return agentic.ToolResult{ID: call.ID, Name: call.Name, Output: toJSON(map[string]any{
		"path":        resolved,
		"entries":     items,
		"truncated":   truncated,
		"max_entries": limit,
	})}, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
