package components

import (
	"strings"
	"unicode/utf8"

	"github.com/victorarias/agentic-weave/cmd/wv/tui"
)

// Editor is a minimal line editor with cursor support.
type Editor struct {
	Prompt      string
	Placeholder string

	buffer []rune
	cursor int

	pendingInput []byte
	skipNextLF   bool

	onSubmit func(string)
}

// NewEditor creates a new editor.
func NewEditor(prompt string) *Editor {
	if prompt == "" {
		prompt = "> "
	}
	return &Editor{Prompt: prompt, Placeholder: "Send a message (Enter to submit, Ctrl+C to exit)"}
}

// SetSubmitHandler sets callback for Enter.
func (e *Editor) SetSubmitHandler(fn func(string)) {
	e.onSubmit = fn
}

// Value returns current buffer.
func (e *Editor) Value() string {
	return string(e.buffer)
}

// Clear clears editor contents.
func (e *Editor) Clear() {
	e.buffer = e.buffer[:0]
	e.cursor = 0
	e.pendingInput = e.pendingInput[:0]
	e.skipNextLF = false
}

// Render renders the editor content.
func (e *Editor) Render(width int) []string {
	content := e.Value()
	if content == "" {
		content = e.Placeholder
	}
	lines := hardWrap(e.Prompt+content, width)
	if len(lines) == 0 {
		return []string{e.Prompt}
	}
	return lines
}

// Cursor returns cursor position in rendered coordinates.
func (e *Editor) Cursor(width int) (int, int, bool) {
	if width <= 0 {
		return 1, 1, true
	}
	prefix := e.Prompt + string(e.buffer[:e.cursor])
	lines := hardWrap(prefix, width)
	if len(lines) == 0 {
		return 1, 1, true
	}
	row := len(lines)
	col := tui.VisibleWidth(lines[row-1]) + 1
	if col <= 0 {
		col = 1
	}
	return row, col, true
}

// HandleInput processes raw key input.
func (e *Editor) HandleInput(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	e.pendingInput = append(e.pendingInput, data...)

	consumed := false
	for len(e.pendingInput) > 0 {
		if e.pendingInput[0] == 0x1b {
			n, handled, waitForMore := parseEscape(e.pendingInput)
			if waitForMore {
				break
			}
			if handled {
				e.applyEscape(e.pendingInput[:n])
				consumed = true
			}
			e.pendingInput = e.pendingInput[n:]
			continue
		}

		b := e.pendingInput[0]
		switch b {
		case '\r':
			e.submitAndClear()
			e.skipNextLF = true
			e.pendingInput = e.pendingInput[1:]
			consumed = true
			continue
		case '\n':
			e.pendingInput = e.pendingInput[1:]
			if e.skipNextLF {
				e.skipNextLF = false
				consumed = true
				continue
			}
			e.submitAndClear()
			consumed = true
			continue
		case 127, 8:
			e.backspace()
			e.pendingInput = e.pendingInput[1:]
			e.skipNextLF = false
			consumed = true
			continue
		case 1: // Ctrl+A
			e.cursor = 0
			e.pendingInput = e.pendingInput[1:]
			e.skipNextLF = false
			consumed = true
			continue
		case 5: // Ctrl+E
			e.cursor = len(e.buffer)
			e.pendingInput = e.pendingInput[1:]
			e.skipNextLF = false
			consumed = true
			continue
		case 21: // Ctrl+U
			e.buffer = append([]rune{}, e.buffer[e.cursor:]...)
			e.cursor = 0
			e.pendingInput = e.pendingInput[1:]
			e.skipNextLF = false
			consumed = true
			continue
		case 11: // Ctrl+K
			e.buffer = append([]rune{}, e.buffer[:e.cursor]...)
			e.pendingInput = e.pendingInput[1:]
			e.skipNextLF = false
			consumed = true
			continue
		case 23: // Ctrl+W
			e.deleteWordBackward()
			e.pendingInput = e.pendingInput[1:]
			e.skipNextLF = false
			consumed = true
			continue
		}

		if b < 32 {
			e.pendingInput = e.pendingInput[1:]
			e.skipNextLF = false
			continue
		}
		if !utf8.FullRune(e.pendingInput) {
			break
		}
		r, size := utf8.DecodeRune(e.pendingInput)
		if r == utf8.RuneError && size == 1 {
			e.pendingInput = e.pendingInput[1:]
			e.skipNextLF = false
			continue
		}
		e.insertRune(r)
		e.pendingInput = e.pendingInput[size:]
		e.skipNextLF = false
		consumed = true
	}

	return consumed
}

func (e *Editor) submitAndClear() {
	if e.onSubmit != nil {
		e.onSubmit(strings.TrimSpace(e.Value()))
	}
	e.buffer = e.buffer[:0]
	e.cursor = 0
}

func (e *Editor) applyEscape(seq []byte) {
	if len(seq) == 0 || seq[0] != 0x1b {
		return
	}
	if len(seq) >= 3 && seq[1] == '[' {
		switch seq[len(seq)-1] {
		case 'D':
			e.moveLeft()
		case 'C':
			e.moveRight()
		case 'H':
			e.cursor = 0
		case 'F':
			e.cursor = len(e.buffer)
		}
		return
	}
	if len(seq) == 3 && seq[1] == 'O' {
		switch seq[2] {
		case 'H':
			e.cursor = 0
		case 'F':
			e.cursor = len(e.buffer)
		}
	}
}

func parseEscape(data []byte) (n int, handled bool, waitForMore bool) {
	if len(data) < 2 {
		return 0, false, true
	}
	if data[1] == '[' || data[1] == 'O' {
		for i := 2; i < len(data); i++ {
			if data[i] >= '@' && data[i] <= '~' {
				if data[1] == '[' {
					switch data[i] {
					case 'D', 'C', 'H', 'F':
						return i + 1, true, false
					}
					return i + 1, false, false
				}
				if data[1] == 'O' {
					switch data[i] {
					case 'H', 'F':
						return i + 1, true, false
					}
					return i + 1, false, false
				}
			}
		}
		return 0, false, true
	}
	// Treat Alt+key as plain key by consuming just ESC and allowing the following byte to parse normally.
	return 1, false, false
}

func (e *Editor) moveLeft() {
	if e.cursor > 0 {
		e.cursor--
	}
}

func (e *Editor) moveRight() {
	if e.cursor < len(e.buffer) {
		e.cursor++
	}
}

func (e *Editor) backspace() {
	if e.cursor == 0 {
		return
	}
	e.buffer = append(e.buffer[:e.cursor-1], e.buffer[e.cursor:]...)
	e.cursor--
}

func (e *Editor) deleteWordBackward() {
	if e.cursor == 0 {
		return
	}
	start := e.cursor
	for start > 0 && e.buffer[start-1] == ' ' {
		start--
	}
	for start > 0 && e.buffer[start-1] != ' ' {
		start--
	}
	e.buffer = append(e.buffer[:start], e.buffer[e.cursor:]...)
	e.cursor = start
}

func (e *Editor) insertRune(r rune) {
	if e.cursor >= len(e.buffer) {
		e.buffer = append(e.buffer, r)
		e.cursor = len(e.buffer)
		return
	}
	e.buffer = append(e.buffer[:e.cursor], append([]rune{r}, e.buffer[e.cursor:]...)...)
	e.cursor++
}

func hardWrap(value string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	runes := []rune(value)
	if len(runes) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, (len(runes)+width-1)/width)
	start := 0
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\n' {
			lines = append(lines, string(runes[start:i]))
			start = i + 1
			continue
		}
		if i-start+1 >= width {
			lines = append(lines, string(runes[start:i+1]))
			start = i + 1
		}
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}
