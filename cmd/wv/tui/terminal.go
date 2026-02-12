package tui

import (
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/term"
)

// Terminal wraps basic terminal io and raw-mode lifecycle.
type Terminal struct {
	in  *os.File
	out io.Writer

	fd       int
	rawState *term.State
	mu       sync.Mutex
}

// NewTerminal binds stdin/stdout.
func NewTerminal(in *os.File, out io.Writer) *Terminal {
	return &Terminal{in: in, out: out, fd: int(in.Fd())}
}

// EnterRawMode switches the terminal to raw mode.
func (t *Terminal) EnterRawMode() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.rawState != nil {
		return nil
	}
	state, err := term.MakeRaw(t.fd)
	if err != nil {
		return err
	}
	t.rawState = state
	return nil
}

// ExitRawMode restores the terminal mode.
func (t *Terminal) ExitRawMode() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.rawState == nil {
		return nil
	}
	if err := term.Restore(t.fd, t.rawState); err != nil {
		return err
	}
	t.rawState = nil
	return nil
}

// Size returns terminal dimensions.
func (t *Terminal) Size() (width int, height int) {
	w, h, err := term.GetSize(t.fd)
	if err != nil || w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

// Width returns terminal width.
func (t *Terminal) Width() int {
	w, _ := t.Size()
	return w
}

// Read reads bytes from terminal input.
func (t *Terminal) Read(data []byte) (int, error) {
	return t.in.Read(data)
}

// Write writes to terminal output.
func (t *Terminal) Write(value string) error {
	_, err := io.WriteString(t.out, value)
	return err
}

func (t *Terminal) moveCursor(row, col int) error {
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	return t.Write(fmt.Sprintf("\x1b[%d;%dH", row, col))
}
