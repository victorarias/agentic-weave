package tui

import (
	"bytes"
	"os"
	"testing"
)

func TestTerminalWriteAndMoveCursor(t *testing.T) {
	var out bytes.Buffer
	term := NewTerminal(os.Stdin, &out)
	if err := term.Write("hello"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := term.moveCursor(0, -1); err != nil {
		t.Fatalf("moveCursor: %v", err)
	}
	if got := out.String(); got != "hello\x1b[1;1H" {
		t.Fatalf("unexpected terminal output %q", got)
	}
}
