package tui

// Component renders terminal lines constrained to width.
type Component interface {
	Render(width int) []string
}

// InputHandler can consume raw terminal bytes.
type InputHandler interface {
	HandleInput(data []byte) bool
}

// CursorProvider exposes cursor placement after rendering.
type CursorProvider interface {
	Cursor(width int) (row int, col int, ok bool)
}
