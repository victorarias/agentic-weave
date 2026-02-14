package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type staticComponent struct {
	lines []string
}

func (c *staticComponent) Render(_ int) []string {
	out := make([]string, len(c.lines))
	copy(out, c.lines)
	return out
}

type captureHandler struct {
	mu    sync.Mutex
	calls [][]byte
}

func (h *captureHandler) HandleInput(data []byte) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	payload := make([]byte, len(data))
	copy(payload, data)
	h.calls = append(h.calls, payload)
	return true
}

func (h *captureHandler) Joined() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var b strings.Builder
	for _, call := range h.calls {
		b.Write(call)
	}
	return b.String()
}

func TestRenderUsesDiffAfterInitialFrame(t *testing.T) {
	term := NewVirtualTerminal(40)
	root := &staticComponent{lines: []string{"line 1", "line 2", "line 3"}}
	ui := New(term, root)

	if err := ui.Render(); err != nil {
		t.Fatalf("first render: %v", err)
	}
	first := term.LastWrite()
	if !strings.Contains(first, "\x1b[H\x1b[2J") {
		t.Fatalf("expected full render clear sequence, got %q", first)
	}

	term.ResetOutput()
	root.lines[1] = "line two"

	if err := ui.Render(); err != nil {
		t.Fatalf("second render: %v", err)
	}
	second := term.LastWrite()
	if strings.Contains(second, "\x1b[H\x1b[2J") {
		t.Fatalf("did not expect full clear on diff render, got %q", second)
	}
	if !strings.Contains(second, "\x1b[2;1H") {
		t.Fatalf("expected cursor jump to first changed line, got %q", second)
	}
	if !strings.Contains(second, "line two") {
		t.Fatalf("expected changed line in output, got %q", second)
	}
}

func TestRenderWidthChangeForcesFullRender(t *testing.T) {
	term := NewVirtualTerminal(40)
	root := &staticComponent{lines: []string{"alpha", "beta"}}
	ui := New(term, root)

	if err := ui.Render(); err != nil {
		t.Fatalf("first render: %v", err)
	}
	term.ResetOutput()

	term.Resize(120)
	if err := ui.Render(); err != nil {
		t.Fatalf("width change render: %v", err)
	}
	out := term.LastWrite()
	if !strings.Contains(out, "\x1b[H\x1b[2J") {
		t.Fatalf("expected full clear after width change, got %q", out)
	}
}

func TestRunDispatchesInputAndStopsOnCtrlC(t *testing.T) {
	term := NewVirtualTerminal(40)
	root := &staticComponent{lines: []string{"ready"}}
	handler := &captureHandler{}
	ui := New(term, root, handler)

	go func() {
		time.Sleep(15 * time.Millisecond)
		term.PushInputString("hi")
		time.Sleep(15 * time.Millisecond)
		term.PushInput([]byte{3})
	}()

	err := ui.Run(context.Background(), 5*time.Millisecond)
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("expected ErrInterrupted, got %v", err)
	}
	if got := handler.Joined(); !strings.Contains(got, "hi") {
		t.Fatalf("expected handler to receive typed bytes, got %q", got)
	}
	if term.RawModeActive() {
		t.Fatal("expected raw mode to be restored on exit")
	}
}

func TestRunCanOnlyBeCalledOnce(t *testing.T) {
	term := NewVirtualTerminal(40)
	root := &staticComponent{lines: []string{"ready"}}
	ui := New(term, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ui.Run(ctx, 5*time.Millisecond); err != nil {
		t.Fatalf("first run should exit cleanly, got %v", err)
	}
	if err := ui.Run(context.Background(), 5*time.Millisecond); !errors.Is(err, ErrRunAlreadyStarted) {
		t.Fatalf("expected ErrRunAlreadyStarted, got %v", err)
	}
}
