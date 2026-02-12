package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var ErrInterrupted = errors.New("tui: interrupted")
var ErrRunAlreadyStarted = errors.New("tui: run can only be called once")

// TUI manages differential rendering and input dispatch.
type TUI struct {
	term terminalIO

	mu       sync.RWMutex
	root     Component
	handlers []InputHandler
	onTick   func()

	renderMu      sync.Mutex
	previousLines []string
	previousWidth int
	runStarted    bool
}

type terminalIO interface {
	EnterRawMode() error
	ExitRawMode() error
	Width() int
	Read(data []byte) (int, error)
	Write(value string) error
}

// New creates a TUI runtime.
func New(term terminalIO, root Component, handlers ...InputHandler) *TUI {
	return &TUI{term: term, root: root, handlers: handlers}
}

// SetRoot updates the render root.
func (t *TUI) SetRoot(root Component) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.root = root
}

// SetOnTick sets a callback invoked before each render cycle.
func (t *TUI) SetOnTick(fn func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onTick = fn
}

// Render draws the current frame.
func (t *TUI) Render() error {
	t.renderMu.Lock()
	defer t.renderMu.Unlock()

	t.mu.RLock()
	root := t.root
	t.mu.RUnlock()
	if root == nil {
		return nil
	}

	width := t.term.Width()
	lines := root.Render(width)
	if len(lines) == 0 {
		lines = []string{""}
	}

	if t.previousWidth != width || len(t.previousLines) == 0 {
		if err := t.fullRender(width, lines, root); err != nil {
			return err
		}
		t.previousWidth = width
		t.previousLines = cloneLines(lines)
		return nil
	}

	first := firstDiff(t.previousLines, lines)
	if first == -1 {
		return nil
	}

	if err := t.diffRender(width, first, lines, root); err != nil {
		return err
	}
	t.previousLines = cloneLines(lines)
	return nil
}

// Run enters raw mode, processes input, and continuously renders frames.
func (t *TUI) Run(ctx context.Context, frameInterval time.Duration) error {
	t.mu.Lock()
	if t.runStarted {
		t.mu.Unlock()
		return ErrRunAlreadyStarted
	}
	t.runStarted = true
	t.mu.Unlock()

	if frameInterval <= 0 {
		frameInterval = 33 * time.Millisecond
	}
	if err := t.term.EnterRawMode(); err != nil {
		return err
	}
	defer t.term.ExitRawMode()

	_ = t.term.Write("\x1b[?1049h\x1b[?25l")
	defer t.term.Write("\x1b[?25h\x1b[?1049l")

	if err := t.Render(); err != nil {
		return err
	}

	inputCh := make(chan []byte, 64)
	errCh := make(chan error, 1)
	go t.readInput(inputCh, errCh)

	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errCh:
			if err != nil {
				return err
			}
		case data := <-inputCh:
			if containsCtrlC(data) {
				return ErrInterrupted
			}
			t.dispatchInput(data)
			t.invokeTick()
			if err := t.Render(); err != nil {
				return err
			}
		case <-ticker.C:
			t.invokeTick()
			if err := t.Render(); err != nil {
				return err
			}
		}
	}
}

func (t *TUI) fullRender(width int, lines []string, root Component) error {
	var b strings.Builder
	b.WriteString("\x1b[?2026h")
	b.WriteString("\x1b[H\x1b[2J")
	for i, line := range lines {
		b.WriteString("\x1b[2K")
		b.WriteString(clampLine(line, width))
		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	appendCursor(&b, root, width)
	b.WriteString("\x1b[?2026l")
	return t.term.Write(b.String())
}

func (t *TUI) diffRender(width, first int, lines []string, root Component) error {
	maxLines := len(lines)
	if len(t.previousLines) > maxLines {
		maxLines = len(t.previousLines)
	}

	var b strings.Builder
	b.WriteString("\x1b[?2026h")
	b.WriteString("\x1b[")
	b.WriteString(intToString(first + 1))
	b.WriteString(";1H")

	for i := first; i < maxLines; i++ {
		value := ""
		if i < len(lines) {
			value = clampLine(lines[i], width)
		}
		b.WriteString("\x1b[2K")
		b.WriteString(value)
		if i < maxLines-1 {
			b.WriteByte('\n')
		}
	}
	appendCursor(&b, root, width)
	b.WriteString("\x1b[?2026l")
	return t.term.Write(b.String())
}

func appendCursor(b *strings.Builder, root Component, width int) {
	provider, ok := root.(CursorProvider)
	if !ok {
		return
	}
	row, col, ok := provider.Cursor(width)
	if !ok {
		return
	}
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	b.WriteString("\x1b[")
	b.WriteString(intToString(row))
	b.WriteString(";")
	b.WriteString(intToString(col))
	b.WriteString("H")
}

func firstDiff(prev, next []string) int {
	maxLen := len(prev)
	if len(next) > maxLen {
		maxLen = len(next)
	}
	for i := 0; i < maxLen; i++ {
		var a, b string
		if i < len(prev) {
			a = prev[i]
		}
		if i < len(next) {
			b = next[i]
		}
		if a != b {
			return i
		}
	}
	return -1
}

func (t *TUI) dispatchInput(data []byte) {
	t.mu.RLock()
	handlers := append([]InputHandler(nil), t.handlers...)
	t.mu.RUnlock()
	for _, handler := range handlers {
		if handler != nil && handler.HandleInput(data) {
			return
		}
	}
}

func (t *TUI) invokeTick() {
	t.mu.RLock()
	onTick := t.onTick
	t.mu.RUnlock()
	if onTick != nil {
		onTick()
	}
}

func (t *TUI) readInput(inputCh chan<- []byte, errCh chan<- error) {
	buf := make([]byte, 64)
	for {
		n, err := t.term.Read(buf)
		if err != nil {
			errCh <- err
			return
		}
		if n == 0 {
			continue
		}
		payload := make([]byte, n)
		copy(payload, buf[:n])
		inputCh <- payload
	}
}

func containsCtrlC(data []byte) bool {
	for _, b := range data {
		if b == 3 {
			return true
		}
	}
	return false
}

func clampLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if VisibleWidth(line) <= width {
		return line
	}
	return TruncateVisible(line, width)
}

func cloneLines(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
