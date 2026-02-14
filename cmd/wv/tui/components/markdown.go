package components

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/victorarias/agentic-weave/cmd/wv/tui"
)

// Markdown renders markdown-ish text, including fenced code blocks.
type Markdown struct {
	Value string
}

// NewMarkdown creates a markdown component.
func NewMarkdown(value string) *Markdown {
	return &Markdown{Value: value}
}

// Set updates markdown content.
func (m *Markdown) Set(value string) {
	m.Value = value
}

// Render renders markdown content into terminal lines.
func (m *Markdown) Render(width int) []string {
	trimmed := strings.TrimSpace(m.Value)
	if trimmed == "" {
		return []string{""}
	}

	rawLines := strings.Split(m.Value, "\n")
	out := make([]string, 0, len(rawLines))

	inCode := false
	lang := "plaintext"
	code := make([]string, 0)

	for _, line := range rawLines {
		marker := strings.TrimSpace(line)
		if strings.HasPrefix(marker, "```") {
			if inCode {
				out = append(out, renderCodeBlock(strings.Join(code, "\n"), lang)...)
				inCode = false
				lang = "plaintext"
				code = code[:0]
				continue
			}
			inCode = true
			if len(marker) > 3 {
				lang = strings.TrimSpace(marker[3:])
			}
			continue
		}

		if inCode {
			code = append(code, line)
			continue
		}

		wrapped := tui.WrapText(line, width)
		out = append(out, wrapped...)
	}

	if inCode {
		out = append(out, renderCodeBlock(strings.Join(code, "\n"), lang)...)
	}

	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func renderCodeBlock(code, lang string) []string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = "plaintext"
	}

	var buf bytes.Buffer
	if err := quick.Highlight(&buf, code, lang, "terminal", "monokai"); err != nil {
		return strings.Split(code, "\n")
	}
	highlighted := strings.TrimSuffix(buf.String(), "\n")
	if highlighted == "" {
		return []string{""}
	}
	return strings.Split(highlighted, "\n")
}
