package components

import "testing"

func TestTextSetAndRender(t *testing.T) {
	c := NewText("hello world")
	lines := c.Render(5)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output, got %#v", lines)
	}
	c.Set("x")
	lines = c.Render(5)
	if len(lines) != 1 || lines[0] != "x" {
		t.Fatalf("unexpected text render %#v", lines)
	}
}
