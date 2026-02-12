package tools

import (
	"context"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
)

type globInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	MaxMatches int    `json:"max_matches,omitempty"`
}

// GlobTool finds files by glob pattern.
type GlobTool struct {
	WorkDir string
}

func (t GlobTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        "glob",
		Description: "Find files matching a glob pattern.",
		InputSchema: toJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "Glob pattern, e.g. *.go or **/*.md."},
				"path":        map[string]any{"type": "string", "description": "Optional base path."},
				"max_matches": map[string]any{"type": "integer", "description": "Maximum number of returned matches."},
			},
			"required": []string{"pattern"},
		}),
	}
}

func (t GlobTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	var input globInput
	if res := parseInput(call, &input); res != nil {
		return *res, nil
	}
	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: "pattern is required"}}, nil
	}

	base := input.Path
	if strings.TrimSpace(base) == "" {
		base = "."
	}
	root, err := resolvePath(t.WorkDir, base)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	limit := sanitizeLimit(input.MaxMatches, 1000, 10000)
	normalizedPattern := normalizeGlobPattern(pattern)
	workspaceRoot := normalizeWorkDir(t.WorkDir)
	results := make([]string, 0, 64)
	truncated := false

	addMatch := func(path string) bool {
		rel, relErr := filepath.Rel(workspaceRoot, path)
		if relErr != nil {
			rel = path
		}
		results = append(results, rel)
		if len(results) >= limit {
			truncated = true
			return true
		}
		return false
	}

	info, err := os.Stat(root)
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}

	if info.Mode().IsRegular() {
		if globMatch(normalizedPattern, filepath.Base(root)) {
			_ = addMatch(root)
		}
	} else {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return filepath.SkipAll
			}
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if globMatch(normalizedPattern, rel) {
				if addMatch(path) {
					return filepath.SkipAll
				}
			}
			return nil
		})
	}
	sort.Strings(results)

	return agentic.ToolResult{ID: call.ID, Name: call.Name, Output: toJSON(map[string]any{
		"pattern":     pattern,
		"base":        root,
		"matches":     uniqueStrings(results),
		"max_matches": limit,
		"truncated":   truncated,
	})}, nil
}

func normalizeGlobPattern(pattern string) string {
	cleaned := strings.TrimSpace(pattern)
	cleaned = strings.TrimPrefix(cleaned, "./")
	cleaned = strings.TrimPrefix(cleaned, ".\\")
	return filepath.ToSlash(cleaned)
}

func globMatch(pattern, value string) bool {
	if pattern == "" {
		return false
	}
	patternParts := strings.Split(pattern, "/")
	valueParts := strings.Split(filepath.ToSlash(value), "/")
	return globMatchParts(patternParts, valueParts, 0, 0)
}

func globMatchParts(patternParts, valueParts []string, pi, vi int) bool {
	if pi == len(patternParts) {
		return vi == len(valueParts)
	}
	if patternParts[pi] == "**" {
		if pi+1 == len(patternParts) {
			return true
		}
		for i := vi; i <= len(valueParts); i++ {
			if globMatchParts(patternParts, valueParts, pi+1, i) {
				return true
			}
		}
		return false
	}
	if vi >= len(valueParts) {
		return false
	}
	ok, _ := pathpkg.Match(patternParts[pi], valueParts[vi])
	if !ok {
		return false
	}
	return globMatchParts(patternParts, valueParts, pi+1, vi+1)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
