package components

import "testing"

type staticComp struct{ lines []string }

func (s staticComp) Render(_ int) []string { return s.lines }

type inputComp struct{ consumed bool }

func (i *inputComp) Render(_ int) []string { return []string{"x"} }
func (i *inputComp) HandleInput(_ []byte) bool {
	i.consumed = true
	return true
}

type cursorComp struct{}

func (cursorComp) Render(_ int) []string         { return []string{"a", "b"} }
func (cursorComp) Cursor(_ int) (int, int, bool) { return 2, 3, true }

func TestContainerRenderAndInputAndCursor(t *testing.T) {
	inp := &inputComp{}
	c := NewContainer(staticComp{lines: []string{"one"}}, inp, cursorComp{})
	lines := c.Render(80)
	if len(lines) != 4 {
		t.Fatalf("expected combined lines, got %#v", lines)
	}
	if !c.HandleInput([]byte("x")) || !inp.consumed {
		t.Fatal("expected input to be consumed")
	}
	row, col, ok := c.Cursor(80)
	if !ok || row != 4 || col != 3 {
		t.Fatalf("unexpected cursor location row=%d col=%d ok=%v", row, col, ok)
	}
}
