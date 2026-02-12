package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
)

const testBashTimeout = 10 * time.Second

func executeTool(t *testing.T, tool agentic.Tool, input any) agentic.ToolResult {
	t.Helper()
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	result, err := tool.Execute(context.Background(), agentic.ToolCall{Name: tool.Definition().Name, Input: payload})
	if err != nil {
		t.Fatalf("execute tool: %v", err)
	}
	return result
}

func executeBashSuccess(t *testing.T, tool BashTool, input map[string]any) agentic.ToolResult {
	t.Helper()
	for attempt := 1; attempt <= 3; attempt++ {
		result := executeTool(t, tool, input)
		if result.Error == nil {
			return result
		}
		msg := strings.ToLower(result.Error.Message)
		if strings.Contains(msg, "resource temporarily unavailable") && attempt < 3 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		t.Fatalf("unexpected bash error: %v", result.Error)
	}
	t.Fatal("bash success helper exhausted retries")
	return agentic.ToolResult{}
}

func decodeOutput(t *testing.T, result agentic.ToolResult) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(result.Output, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	return out
}

func TestWriteReadEditFlow(t *testing.T) {
	workDir := t.TempDir()

	writeResult := executeTool(t, WriteTool{WorkDir: workDir}, map[string]any{
		"path":    "notes.txt",
		"content": "alpha\nbeta\n",
	})
	if writeResult.Error != nil {
		t.Fatalf("write error: %v", writeResult.Error)
	}

	readResult := executeTool(t, ReadTool{WorkDir: workDir}, map[string]any{
		"path":       "notes.txt",
		"start_line": 2,
		"end_line":   2,
	})
	readOut := decodeOutput(t, readResult)
	if got := readOut["content"].(string); got != "beta" {
		t.Fatalf("expected line slice beta, got %q", got)
	}

	editResult := executeTool(t, EditTool{WorkDir: workDir}, map[string]any{
		"path":       "notes.txt",
		"old_string": "beta",
		"new_string": "gamma",
	})
	if editResult.Error != nil {
		t.Fatalf("edit error: %v", editResult.Error)
	}

	data, err := os.ReadFile(filepath.Join(workDir, "notes.txt"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "gamma") {
		t.Fatalf("expected file to contain replacement, got %q", string(data))
	}

	outOfRange := executeTool(t, ReadTool{WorkDir: workDir}, map[string]any{
		"path":       "notes.txt",
		"start_line": 99,
		"end_line":   100,
	})
	outOfRangePayload := decodeOutput(t, outOfRange)
	if got := outOfRangePayload["content"].(string); got != "" {
		t.Fatalf("expected empty content for out-of-range lines, got %q", got)
	}
}

func TestGrepGlobAndLS(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "a.go"), []byte("package main\n// needle\n"), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "b.txt"), []byte("nothing\n"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write hidden: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "nested", "deep.go"), []byte("package nested\n"), 0o644); err != nil {
		t.Fatalf("write deep.go: %v", err)
	}

	globResult := executeTool(t, GlobTool{WorkDir: workDir}, map[string]any{"pattern": "*.go"})
	globOut := decodeOutput(t, globResult)
	matches := globOut["matches"].([]any)
	if len(matches) != 1 || !strings.HasSuffix(matches[0].(string), "a.go") {
		t.Fatalf("unexpected glob matches: %#v", matches)
	}
	globDeep := executeTool(t, GlobTool{WorkDir: workDir}, map[string]any{"pattern": "**/*.go"})
	globDeepOut := decodeOutput(t, globDeep)
	deepMatches := globDeepOut["matches"].([]any)
	if len(deepMatches) != 2 {
		t.Fatalf("expected recursive glob to find 2 files, got %#v", deepMatches)
	}

	grepResult := executeTool(t, GrepTool{WorkDir: workDir}, map[string]any{"pattern": "needle", "path": ".", "glob": "*.go"})
	grepOut := decodeOutput(t, grepResult)
	grepMatches := grepOut["matches"].([]any)
	if len(grepMatches) != 1 {
		t.Fatalf("expected one grep match, got %#v", grepMatches)
	}

	lsResult := executeTool(t, LSTool{WorkDir: workDir}, map[string]any{"path": "."})
	lsOut := decodeOutput(t, lsResult)
	entries := lsOut["entries"].([]any)
	for _, item := range entries {
		name := item.(map[string]any)["name"].(string)
		if name == ".hidden" {
			t.Fatal("hidden file should be excluded by default")
		}
	}

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("needle outside"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	link := filepath.Join(workDir, "link-outside.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	escaped := executeTool(t, GrepTool{WorkDir: workDir}, map[string]any{"pattern": "needle", "path": "."})
	escapedOut := decodeOutput(t, escaped)
	escapedMatches := escapedOut["matches"].([]any)
	for _, m := range escapedMatches {
		path := m.(map[string]any)["path"].(string)
		if strings.Contains(path, "link-outside.txt") {
			t.Fatalf("symlinked outside file should not be searched, got match %#v", m)
		}
	}
}

func TestBashTool(t *testing.T) {
	workDir := t.TempDir()
	tool := BashTool{WorkDir: workDir, Timeout: testBashTimeout}

	okResult := executeBashSuccess(t, tool, map[string]any{"command": "printf 'ok'"})
	okOut := decodeOutput(t, okResult)
	if okOut["stdout"].(string) != "ok" {
		t.Fatalf("expected stdout ok, got %#v", okOut)
	}

	failResult := executeTool(t, tool, map[string]any{"command": "exit 7"})
	if failResult.Error == nil {
		t.Fatal("expected non-zero command to return tool error")
	}
	failOut := decodeOutput(t, failResult)
	if failOut["exit_code"].(float64) != 7 {
		t.Fatalf("expected exit code 7, got %#v", failOut)
	}
}

func TestToolPathsAreConstrainedToWorkspace(t *testing.T) {
	workDir := t.TempDir()
	outside := filepath.Join(workDir, "..", "outside.txt")

	writeResult := executeTool(t, WriteTool{WorkDir: workDir}, map[string]any{
		"path":    outside,
		"content": "x",
	})
	if writeResult.Error == nil {
		t.Fatal("expected outside-workspace write to fail")
	}

	readResult := executeTool(t, ReadTool{WorkDir: workDir}, map[string]any{"path": "../outside.txt"})
	if readResult.Error == nil {
		t.Fatal("expected outside-workspace read to fail")
	}

	bashResult := executeTool(t, BashTool{WorkDir: workDir, Timeout: testBashTimeout}, map[string]any{
		"command":  "pwd",
		"work_dir": "..",
	})
	if bashResult.Error == nil {
		t.Fatal("expected outside-workspace bash work_dir to fail")
	}

	outsideDir := t.TempDir()
	linkPath := filepath.Join(workDir, "link")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	symlinkWrite := executeTool(t, WriteTool{WorkDir: workDir}, map[string]any{
		"path":    filepath.Join("link", "new", "file.txt"),
		"content": "x",
	})
	if symlinkWrite.Error == nil {
		t.Fatal("expected symlink-prefix write to fail")
	}
}

func TestBashOutputIsBounded(t *testing.T) {
	workDir := t.TempDir()
	tool := BashTool{WorkDir: workDir, Timeout: testBashTimeout}
	result := executeBashSuccess(t, tool, map[string]any{
		"command":          "printf '%02000d' 0",
		"max_output_bytes": 128,
	})
	out := decodeOutput(t, result)
	if got := len(out["stdout"].(string)); got != 128 {
		t.Fatalf("expected bounded stdout length 128, got %d", got)
	}
	if !out["stdout_truncated"].(bool) {
		t.Fatal("expected stdout_truncated=true")
	}
}

func TestResolvePathAcceptsPathsInsideSymlinkedWorkspace(t *testing.T) {
	realWorkspace := t.TempDir()
	parent := filepath.Dir(realWorkspace)
	linkPath := filepath.Join(parent, "wv-workspace-link")
	if err := os.Symlink(realWorkspace, linkPath); err != nil {
		t.Fatalf("create workspace symlink: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(linkPath) })

	writeResult := executeTool(t, WriteTool{WorkDir: linkPath}, map[string]any{
		"path":    "inside.txt",
		"content": "ok",
	})
	if writeResult.Error != nil {
		t.Fatalf("expected symlinked workspace path to be accepted, got %v", writeResult.Error)
	}
	if _, err := os.Stat(filepath.Join(realWorkspace, "inside.txt")); err != nil {
		t.Fatalf("expected file created in real workspace: %v", err)
	}
}
