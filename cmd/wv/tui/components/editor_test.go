package components

import (
	"strings"
	"testing"
)

func TestEditorHandlesTypingCursorAndSubmit(t *testing.T) {
	editor := NewEditor("> ")

	editor.HandleInput([]byte("hello"))
	editor.HandleInput([]byte{127}) // backspace
	if got := editor.Value(); got != "hell" {
		t.Fatalf("expected hell after backspace, got %q", got)
	}

	editor.HandleInput([]byte("\x1b[D")) // left arrow
	editor.HandleInput([]byte("X"))
	if got := editor.Value(); got != "helXl" {
		t.Fatalf("expected cursor insert to produce helXl, got %q", got)
	}

	var submitted string
	editor.SetSubmitHandler(func(value string) {
		submitted = value
	})
	editor.HandleInput([]byte("\r"))

	if submitted != "helXl" {
		t.Fatalf("expected submitted text helXl, got %q", submitted)
	}
	if editor.Value() != "" {
		t.Fatalf("expected editor to clear after submit, got %q", editor.Value())
	}
}

func TestEditorConsumesAllPrintableBytesFromSingleRead(t *testing.T) {
	editor := NewEditor("> ")
	if consumed := editor.HandleInput([]byte("abc123")); !consumed {
		t.Fatal("expected input to be consumed")
	}
	if got := editor.Value(); got != "abc123" {
		t.Fatalf("expected full payload to be applied, got %q", got)
	}
}

func TestEditorHandlesSplitEscapeSequences(t *testing.T) {
	editor := NewEditor("> ")
	editor.HandleInput([]byte("abc"))
	editor.HandleInput([]byte("\x1b["))
	editor.HandleInput([]byte("D"))
	editor.HandleInput([]byte("X"))
	if got := editor.Value(); got != "abXc" {
		t.Fatalf("expected split CSI left arrow handling, got %q", got)
	}
}

func TestEditorHandlesUTF8AcrossReads(t *testing.T) {
	editor := NewEditor("> ")
	editor.HandleInput([]byte{0xC3})
	editor.HandleInput([]byte{0xA9}) // "é"
	editor.HandleInput([]byte("z"))
	if got := editor.Value(); got != "éz" {
		t.Fatalf("expected UTF-8 rune assembly across reads, got %q", got)
	}
}

func TestEditorPlaceholderRenderHasNoPartialANSISequences(t *testing.T) {
	editor := NewEditor("> ")
	lines := editor.Render(4)
	for _, line := range lines {
		if strings.Contains(line, "\x1b") {
			t.Fatalf("expected plain placeholder wrapping, got %q", line)
		}
	}
}
