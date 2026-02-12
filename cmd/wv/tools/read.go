package tools

import (
	"bufio"
	"context"
	"os"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
)

type readInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
}

// ReadTool reads file contents with optional line ranges.
type ReadTool struct {
	WorkDir string
}

func (t ReadTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        "read",
		Description: "Read file contents, optionally constrained to a line range.",
		InputSchema: toJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string", "description": "File path to read."},
				"start_line": map[string]any{"type": "integer", "description": "1-based start line."},
				"end_line":   map[string]any{"type": "integer", "description": "1-based end line (inclusive)."},
				"max_bytes":  map[string]any{"type": "integer", "description": "Optional output byte limit."},
			},
			"required": []string{"path"},
		}),
	}
}

func (t ReadTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	var input readInput
	if res := parseInput(call, &input); res != nil {
		return *res, nil
	}
	path, err := resolvePath(t.WorkDir, input.Path)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	defer f.Close()

	maxBytes := sanitizeLimit(input.MaxBytes, 64*1024, 512*1024)
	start := input.StartLine
	if start <= 0 {
		start = 1
	}
	requestedEnd := input.EndLine

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)

	var content strings.Builder
	totalLines := 0
	truncated := false
	captureEnabled := true
	selectedLines := 0

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
		}
		totalLines++
		if totalLines < start {
			continue
		}
		if requestedEnd > 0 && totalLines > requestedEnd {
			continue
		}

		selectedLines++
		if !captureEnabled {
			continue
		}

		line := scanner.Text()
		if selectedLines > 1 {
			if content.Len() >= maxBytes {
				truncated = true
				captureEnabled = false
				continue
			}
			content.WriteByte('\n')
		}

		remaining := maxBytes - content.Len()
		if remaining <= 0 {
			truncated = true
			captureEnabled = false
			continue
		}
		if len(line) > remaining {
			content.WriteString(line[:remaining])
			truncated = true
			captureEnabled = false
			continue
		}
		content.WriteString(line)
	}
	if err := scanner.Err(); err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	if totalLines == 0 {
		totalLines = 1
	}
	end := requestedEnd
	if end <= 0 || end > totalLines {
		end = totalLines
	}
	if start > totalLines {
		start = totalLines + 1
		end = totalLines
	}
	if end < start {
		end = start - 1
	}

	return agentic.ToolResult{ID: call.ID, Name: call.Name, Output: toJSON(map[string]any{
		"path":        path,
		"content":     content.String(),
		"start_line":  start,
		"end_line":    end,
		"total_lines": totalLines,
		"max_bytes":   maxBytes,
		"truncated":   truncated,
	})}, nil
}
