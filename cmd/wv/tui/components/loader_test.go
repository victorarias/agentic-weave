package components

import "testing"

func TestLoaderRender(t *testing.T) {
	l := NewLoader("thinking")
	lines := l.Render(80)
	if len(lines) != 1 {
		t.Fatalf("expected one loader line, got %#v", lines)
	}
	if lines[0] == "" {
		t.Fatal("expected non-empty spinner output")
	}
}
