package turn

import (
	"errors"
	"testing"
)

func TestPinnedPromptValidate_FirstTurnSemantics(t *testing.T) {
	var pinned PinnedPrompt
	if err := pinned.Validate("anything"); err != nil {
		t.Errorf("expected no error on first turn, got %v", err)
	}
	if pinned.IsPinned() {
		t.Error("expected not pinned before Pin()")
	}
}

func TestPinnedPromptValidate_MatchingPrompt(t *testing.T) {
	var pinned PinnedPrompt
	pinned.Pin("system")

	if err := pinned.Validate("system"); err != nil {
		t.Errorf("expected no error for matching prompt, got %v", err)
	}
	if pinned.Content() != "system" {
		t.Errorf("expected content %q, got %q", "system", pinned.Content())
	}
	if !pinned.IsPinned() {
		t.Error("expected pinned after Pin()")
	}
}

func TestPinnedPromptValidate_DifferentPromptReturnsErrPromptChanged(t *testing.T) {
	var pinned PinnedPrompt
	pinned.Pin("system")

	err := pinned.Validate("different")
	if err == nil {
		t.Fatal("expected error for different prompt")
	}
	if !errors.Is(err, ErrPromptChanged) {
		t.Errorf("expected ErrPromptChanged, got %v", err)
	}
}
