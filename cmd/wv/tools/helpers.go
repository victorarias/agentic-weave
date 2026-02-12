package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
)

func normalizeWorkDir(workDir string) string {
	trimmed := strings.TrimSpace(workDir)
	if trimmed == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "."
		}
		trimmed = cwd
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(abs)
}

func resolvePath(workDir, requested string) (string, error) {
	value := strings.TrimSpace(requested)
	if value == "" {
		return "", fmt.Errorf("path is required")
	}
	root := normalizeWorkDir(workDir)
	target := value
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target = filepath.Clean(target)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if err := ensureWithinRoot(root, absTarget); err != nil {
		return "", err
	}
	return absTarget, nil
}

func ensureWithinRoot(root, target string) error {
	rootResolved := resolveExistingPath(root)
	if !isWithinRootLexical(root, target) && !isWithinRootLexical(rootResolved, target) {
		return fmt.Errorf("path %q escapes workspace root %q", target, root)
	}
	if err := ensureExistingPathSegmentsWithinRoot(root, rootResolved, target); err != nil {
		return err
	}
	if eval, err := filepath.EvalSymlinks(target); err == nil {
		if !isWithinRootLexical(rootResolved, eval) {
			return fmt.Errorf("path %q escapes workspace root %q", target, root)
		}
		return nil
	}
	parent := filepath.Dir(target)
	if evalParent, err := filepath.EvalSymlinks(parent); err == nil {
		candidate := filepath.Join(evalParent, filepath.Base(target))
		if !isWithinRootLexical(rootResolved, candidate) {
			return fmt.Errorf("path %q escapes workspace root %q", target, root)
		}
	}
	return nil
}

func ensureExistingPathSegmentsWithinRoot(root, rootResolved, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current := root
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				// Deeper paths cannot exist if this segment does not.
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		evaluated, err := filepath.EvalSymlinks(current)
		if err != nil {
			return err
		}
		if !isWithinRootLexical(rootResolved, evaluated) {
			return fmt.Errorf("path %q escapes workspace root %q", target, root)
		}
	}
	return nil
}

func resolveExistingPath(path string) string {
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return evaluated
}

func isWithinRootLexical(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func sanitizeLimit(requested, fallback, hardMax int) int {
	if fallback <= 0 {
		fallback = hardMax
	}
	if hardMax > 0 && fallback > hardMax {
		fallback = hardMax
	}
	if requested <= 0 {
		return fallback
	}
	if hardMax > 0 && requested > hardMax {
		return hardMax
	}
	return requested
}

func clampBytes(value string, max int) (string, bool) {
	if max <= 0 || len(value) <= max {
		return value, false
	}
	return value[:max], true
}

func toJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func parseInput(call agentic.ToolCall, target any) *agentic.ToolResult {
	if err := json.Unmarshal(call.Input, target); err != nil {
		return &agentic.ToolResult{
			ID:    call.ID,
			Name:  call.Name,
			Error: &agentic.ToolError{Message: "invalid input: " + err.Error()},
		}
	}
	return nil
}
