package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
)

type grepInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	IgnoreCase bool   `json:"ignore_case,omitempty"`
	MaxMatches int    `json:"max_matches,omitempty"`
	MaxBytes   int    `json:"max_bytes,omitempty"`
}

// GrepTool searches for regex matches in files.
type GrepTool struct {
	WorkDir string
}

func (t GrepTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        "grep",
		Description: "Search file contents with a regular expression.",
		InputSchema: toJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "Regular expression pattern."},
				"path":        map[string]any{"type": "string", "description": "Directory or file path."},
				"glob":        map[string]any{"type": "string", "description": "Optional file glob filter."},
				"ignore_case": map[string]any{"type": "boolean", "description": "Case-insensitive matching."},
				"max_matches": map[string]any{"type": "integer", "description": "Optional result cap."},
				"max_bytes":   map[string]any{"type": "integer", "description": "Maximum bytes to read per file."},
			},
			"required": []string{"pattern"},
		}),
	}
}

func (t GrepTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	var input grepInput
	if res := parseInput(call, &input); res != nil {
		return *res, nil
	}
	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: "pattern is required"}}, nil
	}
	if input.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	searchPath := input.Path
	if strings.TrimSpace(searchPath) == "" {
		searchPath = "."
	}
	root, err := resolvePath(t.WorkDir, searchPath)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	type match struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}

	matches := make([]match, 0, 32)
	limit := sanitizeLimit(input.MaxMatches, 200, 5000)
	maxBytes := sanitizeLimit(input.MaxBytes, 128*1024, 1024*1024)
	truncated := false
	skippedLarge := 0

	addMatches := func(path string) error {
		if err := ctx.Err(); err != nil {
			return filepath.SkipAll
		}
		fileInfo, err := os.Lstat(path)
		if err != nil {
			return nil
		}
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil
		}
		if info.Size() > int64(maxBytes) {
			skippedLarge++
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil
		}
		rel, relErr := filepath.Rel(normalizeWorkDir(t.WorkDir), path)
		if relErr != nil {
			rel = path
		}
		if glob := strings.TrimSpace(input.Glob); glob != "" {
			ok, _ := filepath.Match(glob, rel)
			if !ok {
				ok, _ = filepath.Match(glob, filepath.Base(path))
			}
			if !ok {
				return nil
			}
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if err := ctx.Err(); err != nil {
				return filepath.SkipAll
			}
			if !re.MatchString(line) {
				continue
			}
			matches = append(matches, match{Path: rel, Line: i + 1, Text: line})
			if len(matches) >= limit {
				truncated = true
				return filepath.SkipAll
			}
		}
		return nil
	}

	info, err := os.Stat(root)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	if info.Mode().IsRegular() {
		_ = addMatches(root)
	} else {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return filepath.SkipAll
			}
			if walkErr != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			return addMatches(path)
		})
	}

	return agentic.ToolResult{ID: call.ID, Name: call.Name, Output: toJSON(map[string]any{
		"pattern":     input.Pattern,
		"search_path": root,
		"matches":     matches,
		"max_matches": limit,
		"max_bytes":   maxBytes,
		"skipped_big": skippedLarge,
		"truncated":   truncated,
	})}, nil
}
