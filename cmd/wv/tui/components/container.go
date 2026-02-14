package components

import (
	"sync"

	"github.com/victorarias/agentic-weave/cmd/wv/tui"
)

// Container renders child components in order.
type Container struct {
	Children []tui.Component

	mu          sync.RWMutex
	lastWidth   int
	lastHeights []int
}

// NewContainer constructs a container with children.
func NewContainer(children ...tui.Component) *Container {
	return &Container{Children: children}
}

// Render renders all child lines.
func (c *Container) Render(width int) []string {
	lines := make([]string, 0)
	heights := make([]int, len(c.Children))
	for i, child := range c.Children {
		if child != nil {
			childLines := child.Render(width)
			lines = append(lines, childLines...)
			heights[i] = len(childLines)
		} else {
			heights[i] = 0
		}
	}
	c.mu.Lock()
	c.lastWidth = width
	c.lastHeights = heights
	c.mu.Unlock()
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// HandleInput forwards input to child handlers until consumed.
func (c *Container) HandleInput(data []byte) bool {
	for _, child := range c.Children {
		handler, ok := child.(tui.InputHandler)
		if !ok {
			continue
		}
		if handler.HandleInput(data) {
			return true
		}
	}
	return false
}

// Cursor returns the last child cursor offset by its vertical position.
func (c *Container) Cursor(width int) (int, int, bool) {
	c.mu.RLock()
	cachedWidth := c.lastWidth
	cachedHeights := append([]int(nil), c.lastHeights...)
	c.mu.RUnlock()

	useCachedHeights := cachedWidth == width && len(cachedHeights) == len(c.Children)
	rowOffset := 0
	cursorRow := 0
	cursorCol := 0
	found := false

	for i, child := range c.Children {
		if child == nil {
			continue
		}
		if provider, ok := child.(tui.CursorProvider); ok {
			row, col, ok := provider.Cursor(width)
			if ok {
				cursorRow = rowOffset + row
				cursorCol = col
				found = true
			}
		}
		if useCachedHeights {
			rowOffset += cachedHeights[i]
		} else {
			rowOffset += len(child.Render(width))
		}
	}

	return cursorRow, cursorCol, found
}
