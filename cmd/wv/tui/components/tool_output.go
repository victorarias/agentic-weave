package components

import (
	"fmt"
	"strings"

	"github.com/victorarias/agentic-weave/cmd/wv/sanitize"
	"github.com/victorarias/agentic-weave/cmd/wv/tui"
)

// ToolState represents the tool execution status.
type ToolState string

const (
	ToolStatePending ToolState = "pending"
	ToolStateSuccess ToolState = "success"
	ToolStateError   ToolState = "error"
)

// ToolEntry captures a single tool invocation for rendering.
type ToolEntry struct {
	ID       string
	Name     string
	State    ToolState
	Summary  string
	Details  string
	Expanded bool
}

// ToolOutput renders tool execution history with optional details.
type ToolOutput struct {
	entries      []ToolEntry
	expandAll    bool
	maxEntries   int
	fallbackSeed int
}

// NewToolOutput creates an empty tool output component.
func NewToolOutput() *ToolOutput {
	return &ToolOutput{entries: make([]ToolEntry, 0, 32), maxEntries: 24}
}

// AddPending appends a pending tool execution entry.
func (c *ToolOutput) AddPending(id, name string) {
	entryID := c.normalizeID(id, name)
	c.entries = append(c.entries, ToolEntry{
		ID:      entryID,
		Name:    strings.TrimSpace(sanitize.Text(name)),
		State:   ToolStatePending,
		Summary: "running",
	})
	c.trimToMax()
}

// Clear removes all tool output entries.
func (c *ToolOutput) Clear() {
	c.entries = c.entries[:0]
}

// Resolve marks a tool entry as success/error and stores details.
func (c *ToolOutput) Resolve(id, name, summary, details string, isError bool) {
	entryID := c.normalizeID(id, name)
	state := ToolStateSuccess
	if isError {
		state = ToolStateError
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = "completed"
	}
	details = strings.TrimSpace(details)
	name = strings.TrimSpace(sanitize.Text(name))
	summary = strings.TrimSpace(sanitize.Text(summary))
	details = strings.TrimSpace(sanitize.Text(details))

	for i := len(c.entries) - 1; i >= 0; i-- {
		if c.entries[i].ID == entryID {
			c.entries[i].State = state
			c.entries[i].Name = name
			c.entries[i].Summary = summary
			c.entries[i].Details = details
			return
		}
	}

	c.entries = append(c.entries, ToolEntry{
		ID:      entryID,
		Name:    name,
		State:   state,
		Summary: summary,
		Details: details,
	})
	c.trimToMax()
}

// HandleInput toggles details visibility with Ctrl+O.
func (c *ToolOutput) HandleInput(data []byte) bool {
	for _, b := range data {
		if b == 15 { // Ctrl+O
			c.expandAll = !c.expandAll
			return true
		}
	}
	return false
}

// Entries returns a copy of current entries (for tests).
func (c *ToolOutput) Entries() []ToolEntry {
	out := make([]ToolEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

// Render renders tool state lines.
func (c *ToolOutput) Render(width int) []string {
	lines := []string{"Tools (Ctrl+O details):"}
	if len(c.entries) == 0 {
		return append(lines, "  none")
	}

	for _, entry := range c.entries {
		status := "[... ]"
		switch entry.State {
		case ToolStateSuccess:
			status = "[ ok ]"
		case ToolStateError:
			status = "[err ]"
		}
		name := entry.Name
		if name == "" {
			name = "tool"
		}
		summary := entry.Summary
		if summary == "" {
			summary = "completed"
		}
		header := fmt.Sprintf("  %s %s: %s", status, name, summary)
		lines = append(lines, tui.TruncateVisible(header, maxWidth(width, 4)))

		if c.expandAll || entry.Expanded {
			if strings.TrimSpace(entry.Details) == "" {
				continue
			}
			wrapped := tui.WrapText(entry.Details, maxWidth(width-4, 4))
			for _, line := range wrapped {
				lines = append(lines, "    "+line)
			}
		}
	}

	return lines
}

func (c *ToolOutput) trimToMax() {
	if c.maxEntries <= 0 || len(c.entries) <= c.maxEntries {
		return
	}
	start := len(c.entries) - c.maxEntries
	c.entries = append([]ToolEntry(nil), c.entries[start:]...)
}

func (c *ToolOutput) normalizeID(id, name string) string {
	value := strings.TrimSpace(id)
	if value != "" {
		return value
	}
	c.fallbackSeed++
	name = strings.TrimSpace(sanitize.Text(name))
	if name == "" {
		name = "tool"
	}
	return fmt.Sprintf("%s-%d", name, c.fallbackSeed)
}

func maxWidth(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
