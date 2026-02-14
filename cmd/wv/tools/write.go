package tools

import (
	"context"
	"os"
	"path/filepath"

	"github.com/victorarias/agentic-weave/agentic"
)

type writeInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append,omitempty"`
}

// WriteTool writes or appends file content.
type WriteTool struct {
	WorkDir string
}

func (t WriteTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        "write",
		Description: "Write file contents (create or overwrite).",
		InputSchema: toJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path."},
				"content": map[string]any{"type": "string", "description": "Text content."},
				"append":  map[string]any{"type": "boolean", "description": "Append instead of overwrite."},
			},
			"required": []string{"path", "content"},
		}),
	}
}

func (t WriteTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	var input writeInput
	if res := parseInput(call, &input); res != nil {
		return *res, nil
	}
	path, err := resolvePath(t.WorkDir, input.Path)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	flags := os.O_CREATE | os.O_WRONLY
	mode := "overwrite"
	if input.Append {
		flags |= os.O_APPEND
		mode = "append"
	} else {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	defer f.Close()

	n, err := f.WriteString(input.Content)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	return agentic.ToolResult{ID: call.ID, Name: call.Name, Output: toJSON(map[string]any{
		"path":          path,
		"bytes_written": n,
		"mode":          mode,
	})}, nil
}
