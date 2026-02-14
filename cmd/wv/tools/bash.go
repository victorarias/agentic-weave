package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
)

type bashInput struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	WorkDir        string `json:"work_dir,omitempty"`
	MaxOutputBytes int    `json:"max_output_bytes,omitempty"`
}

// BashTool executes shell commands.
type BashTool struct {
	WorkDir string
	Timeout time.Duration
}

func (t BashTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        "bash",
		Description: "Execute a shell command and return stdout/stderr.",
		InputSchema: toJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":         map[string]any{"type": "string", "description": "Shell command to execute."},
				"timeout_seconds": map[string]any{"type": "integer", "description": "Optional timeout override."},
				"work_dir":        map[string]any{"type": "string", "description": "Optional working directory override."},
				"max_output_bytes": map[string]any{
					"type":        "integer",
					"description": "Optional maximum bytes captured for stdout and stderr each.",
				},
			},
			"required": []string{"command"},
		}),
	}
}

func (t BashTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	var input bashInput
	if res := parseInput(call, &input); res != nil {
		return *res, nil
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: "command is required"}}, nil
	}

	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if input.TimeoutSeconds > 0 {
		timeout = time.Duration(input.TimeoutSeconds) * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workingDir, err := resolvePath(t.WorkDir, ".")
	if err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	if strings.TrimSpace(input.WorkDir) != "" {
		workingDir, err = resolvePath(t.WorkDir, input.WorkDir)
		if err != nil {
			return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
		}
	}
	maxOutput := sanitizeLimit(input.MaxOutputBytes, 64*1024, 512*1024)

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = workingDir

	stdout := newLimitedCapture(maxOutput)
	stderr := newLimitedCapture(maxOutput)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	output := map[string]any{
		"command":          command,
		"stdout":           stdout.String(),
		"stderr":           stderr.String(),
		"stdout_truncated": stdout.Truncated(),
		"stderr_truncated": stderr.Truncated(),
		"output_limit":     maxOutput,
		"exit_code":        0,
		"work_dir":         cmd.Dir,
	}
	if err == nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Output: toJSON(output)}, nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		output["exit_code"] = -1
		return agentic.ToolResult{
			ID:     call.ID,
			Name:   call.Name,
			Output: toJSON(output),
			Error:  &agentic.ToolError{Message: fmt.Sprintf("command timed out after %s", timeout)},
		}, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		output["exit_code"] = exitErr.ExitCode()
		return agentic.ToolResult{
			ID:     call.ID,
			Name:   call.Name,
			Output: toJSON(output),
			Error:  &agentic.ToolError{Message: fmt.Sprintf("command failed with exit code %d", exitErr.ExitCode())},
		}, nil
	}

	output["exit_code"] = -1
	return agentic.ToolResult{
		ID:     call.ID,
		Name:   call.Name,
		Output: toJSON(output),
		Error:  &agentic.ToolError{Message: err.Error()},
	}, nil
}

type limitedCapture struct {
	limit     int
	buf       bytes.Buffer
	truncated bool
}

func newLimitedCapture(limit int) limitedCapture {
	if limit < 0 {
		limit = 0
	}
	return limitedCapture{limit: limit}
}

func (c *limitedCapture) Write(p []byte) (int, error) {
	if c.limit == 0 {
		if len(p) > 0 {
			c.truncated = true
		}
		return len(p), nil
	}
	remaining := c.limit - c.buf.Len()
	if remaining <= 0 {
		if len(p) > 0 {
			c.truncated = true
		}
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = c.buf.Write(p[:remaining])
		c.truncated = true
		return len(p), nil
	}
	_, _ = c.buf.Write(p)
	return len(p), nil
}

func (c *limitedCapture) String() string {
	return c.buf.String()
}

func (c *limitedCapture) Truncated() bool {
	return c.truncated
}
