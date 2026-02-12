package tools

import (
	"context"
	"os"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
)

type editInput struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// EditTool performs string replacement in a file.
type EditTool struct {
	WorkDir string
}

func (t EditTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        "edit",
		Description: "Replace text in a file.",
		InputSchema: toJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string", "description": "File path."},
				"old_string":  map[string]any{"type": "string", "description": "Text to replace."},
				"new_string":  map[string]any{"type": "string", "description": "Replacement text."},
				"replace_all": map[string]any{"type": "boolean", "description": "Replace all matches if true."},
			},
			"required": []string{"path", "old_string", "new_string"},
		}),
	}
}

func (t EditTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	var input editInput
	if res := parseInput(call, &input); res != nil {
		return *res, nil
	}
	if input.OldString == "" {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: "old_string is required"}}, nil
	}
	path, err := resolvePath(t.WorkDir, input.Path)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	content := string(data)
	count := strings.Count(content, input.OldString)
	if count == 0 {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: "old_string not found"}}, nil
	}

	replacements := 1
	updated := strings.Replace(content, input.OldString, input.NewString, 1)
	if input.ReplaceAll {
		replacements = count
		updated = strings.ReplaceAll(content, input.OldString, input.NewString)
	}

	if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	return agentic.ToolResult{ID: call.ID, Name: call.Name, Output: toJSON(map[string]any{
		"path":         path,
		"replacements": replacements,
	})}, nil
}
