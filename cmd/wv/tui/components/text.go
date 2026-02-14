package components

import "github.com/victorarias/agentic-weave/cmd/wv/tui"

// Text renders plain text with wrapping.
type Text struct {
	Value string
}

// NewText creates a text component.
func NewText(value string) *Text {
	return &Text{Value: value}
}

// Set updates text content.
func (t *Text) Set(value string) {
	t.Value = value
}

// Render renders wrapped text lines.
func (t *Text) Render(width int) []string {
	return tui.WrapText(t.Value, width)
}
