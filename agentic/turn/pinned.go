package turn

import "errors"

// ErrPromptChanged indicates the system prompt changed mid-conversation.
//
// Mutating the system prompt after conversation start breaks prefix-based prompt
// caching. Use a runtime reminder instead.
var ErrPromptChanged = errors.New(
	"turn: system prompt changed mid-conversation " +
		"(breaks prompt cache); use Reminder instead",
)

// PinnedPrompt tracks a conversation's pinned system prompt.
//
// Comparison is exact byte equality (no normalization).
//
// NOTE: PinnedPrompt is not safe for concurrent mutation without external
// synchronization.
type PinnedPrompt struct {
	content string
	pinned  bool
}

// Pin stores the pinned prompt.
func (p *PinnedPrompt) Pin(content string) {
	p.content = content
	p.pinned = true
}

// Content returns the currently pinned prompt (or empty string if not pinned).
func (p *PinnedPrompt) Content() string {
	return p.content
}

// IsPinned reports whether Pin has been called.
func (p *PinnedPrompt) IsPinned() bool {
	return p.pinned
}

// Validate verifies content against the pinned prompt.
//
// First-turn semantics: if prompt is not pinned yet, Validate returns nil.
func (p *PinnedPrompt) Validate(content string) error {
	if !p.pinned {
		return nil
	}
	if content != p.content {
		return ErrPromptChanged
	}
	return nil
}
