package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/cmd/wv/sanitize"
)

// SummarizeToolUpdate returns a compact summary and details payload for UI display.
func SummarizeToolUpdate(result *agentic.ToolResult) (summary string, details string, isError bool) {
	if result == nil {
		return "", "", false
	}
	if result.Error != nil {
		isError = true
		summary = strings.TrimSpace(sanitize.Text(result.Error.Message))
	}
	details = summarizeToolDetails(result)
	if strings.TrimSpace(summary) == "" {
		summary = summarizeToolResult(result)
	}
	return summary, details, isError
}

func summarizeToolResult(result *agentic.ToolResult) string {
	if result == nil || len(result.Output) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Output, &payload); err != nil {
		return ""
	}

	switch result.Name {
	case "bash":
		stdout, _ := payload["stdout"].(string)
		stderr, _ := payload["stderr"].(string)
		exitCode, _ := payload["exit_code"].(float64)
		text := strings.TrimSpace(sanitize.Text(stdout))
		if text == "" {
			text = strings.TrimSpace(sanitize.Text(stderr))
		}
		if text == "" {
			return fmt.Sprintf("exit=%d", int(exitCode))
		}
		return truncatePreview(text, 120)
	case "read":
		content, _ := payload["content"].(string)
		return truncatePreview(strings.TrimSpace(sanitize.Text(content)), 120)
	case "write", "edit":
		path, _ := payload["path"].(string)
		if path != "" {
			return sanitize.Text(path)
		}
	case "grep":
		if matches, ok := payload["matches"].([]any); ok {
			return fmt.Sprintf("%d matches", len(matches))
		}
	case "glob":
		if matches, ok := payload["matches"].([]any); ok {
			return fmt.Sprintf("%d files", len(matches))
		}
	case "ls":
		if entries, ok := payload["entries"].([]any); ok {
			return fmt.Sprintf("%d entries", len(entries))
		}
	}
	return ""
}

func summarizeToolDetails(result *agentic.ToolResult) string {
	if result == nil || len(result.Output) == 0 {
		return ""
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, result.Output, "", "  "); err == nil {
		return truncatePreview(sanitize.Text(pretty.String()), 800)
	}
	return truncatePreview(sanitize.Text(string(result.Output)), 800)
}

func truncatePreview(value string, max int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
