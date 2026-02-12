package components

import (
	"strings"
	"testing"
)

func TestToolOutputLifecycleAndToggle(t *testing.T) {
	comp := NewToolOutput()
	comp.AddPending("call-1", "read")
	comp.Resolve("call-1", "read", "ok", "line one\nline two", false)

	lines := comp.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[ ok ] read: ok") {
		t.Fatalf("expected resolved header, got %q", joined)
	}
	if strings.Contains(joined, "line one") {
		t.Fatalf("details should be collapsed by default, got %q", joined)
	}

	if consumed := comp.HandleInput([]byte{15}); !consumed {
		t.Fatal("expected Ctrl+O to be consumed")
	}
	lines = comp.Render(80)
	joined = strings.Join(lines, "\n")
	if !strings.Contains(joined, "line one") {
		t.Fatalf("expected details to render when expanded, got %q", joined)
	}
}

func TestToolOutputErrorState(t *testing.T) {
	comp := NewToolOutput()
	comp.AddPending("call-2", "bash")
	comp.Resolve("call-2", "bash", "exit 1", "stderr: failed", true)

	joined := strings.Join(comp.Render(80), "\n")
	if !strings.Contains(joined, "[err ] bash: exit 1") {
		t.Fatalf("expected error status line, got %q", joined)
	}
}

func TestToolOutputSanitizesRenderedFields(t *testing.T) {
	comp := NewToolOutput()
	comp.AddPending("call-3", "ba\x1b[31msh")
	comp.Resolve("call-3", "ba\x1b[31msh", "bad\x1b[0m", "line\x1b]2;title\x07", true)
	joined := strings.Join(comp.Render(120), "\n")
	if strings.Contains(joined, "\x1b") {
		t.Fatalf("expected no terminal escape bytes in output, got %q", joined)
	}
	if !strings.Contains(joined, "ba[31msh") {
		t.Fatalf("expected sanitized visible tool name, got %q", joined)
	}
}
