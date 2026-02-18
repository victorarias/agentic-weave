package anthropic

import (
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

func TestNormalizeThinkingMode(t *testing.T) {
	if got := normalizeThinkingMode(""); got != thinkingModeOff {
		t.Fatalf("expected off for empty mode, got %q", got)
	}
	if got := normalizeThinkingMode("off"); got != thinkingModeOff {
		t.Fatalf("expected off, got %q", got)
	}
	if got := normalizeThinkingMode("fixed"); got != thinkingModeFixed {
		t.Fatalf("expected fixed, got %q", got)
	}
	if got := normalizeThinkingMode("unknown"); got != thinkingModeOff {
		t.Fatalf("expected off fallback, got %q", got)
	}
}

func TestResolveThinkingDefaultsAndOverrides(t *testing.T) {
	c := &Client{thinkingMode: thinkingModeOff, thinkingBgt: 0}
	mode, budget := c.resolveThinking("", 0)
	if mode != thinkingModeOff || budget != 0 {
		t.Fatalf("expected off/0, got %q/%d", mode, budget)
	}

	c = &Client{thinkingMode: thinkingModeFixed, thinkingBgt: 0}
	mode, budget = c.resolveThinking("", 0)
	if mode != thinkingModeFixed || budget < 1024 {
		t.Fatalf("expected fixed budget >= 1024, got %q/%d", mode, budget)
	}

	mode, budget = c.resolveThinking("off", 0)
	if mode != thinkingModeOff || budget != 0 {
		t.Fatalf("expected off/0, got %q/%d", mode, budget)
	}

	mode, budget = c.resolveThinking("fixed", 2048)
	if mode != thinkingModeFixed || budget != 2048 {
		t.Fatalf("expected fixed/2048, got %q/%d", mode, budget)
	}
}

func TestApplyThinkingConfig(t *testing.T) {
	req := sdk.MessageNewParams{}
	applyThinkingConfig(&req, thinkingModeAdaptive, 0)
	if req.Thinking.OfAdaptive == nil {
		t.Fatalf("expected adaptive thinking config")
	}

	req = sdk.MessageNewParams{}
	applyThinkingConfig(&req, thinkingModeOff, 0)
	if req.Thinking.OfDisabled == nil {
		t.Fatalf("expected disabled thinking config")
	}

	req = sdk.MessageNewParams{}
	applyThinkingConfig(&req, thinkingModeFixed, 100)
	if req.Thinking.OfEnabled == nil {
		t.Fatalf("expected fixed thinking config")
	}
	if req.Thinking.OfEnabled.BudgetTokens < 1024 {
		t.Fatalf("expected minimum fixed budget of 1024, got %d", req.Thinking.OfEnabled.BudgetTokens)
	}
}
