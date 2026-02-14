package components

import "time"

// Loader is a tiny spinner component.
type Loader struct {
	Label string
}

var spinnerFrames = []string{"-", "\\", "|", "/"}

// NewLoader creates a new loader.
func NewLoader(label string) *Loader {
	return &Loader{Label: label}
}

// Render renders one spinner frame.
func (l *Loader) Render(width int) []string {
	idx := int(time.Now().UnixNano()/int64(90*time.Millisecond)) % len(spinnerFrames)
	frame := spinnerFrames[idx]
	if l.Label == "" {
		return []string{frame}
	}
	return []string{frame + " " + l.Label}
}
