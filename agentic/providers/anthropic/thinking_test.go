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
	c := &Client{thinkingMode: thinkingModeOff, thinkingEffort: thinkingEffortHigh, thinkingBgt: 0}
	mode, effort, budget := c.resolveThinking("", "", 0)
	if mode != thinkingModeOff || effort != "" || budget != 0 {
		t.Fatalf("expected off/empty/0, got %q/%q/%d", mode, effort, budget)
	}

	c = &Client{thinkingMode: thinkingModeFixed, thinkingEffort: thinkingEffortHigh, thinkingBgt: 0}
	mode, effort, budget = c.resolveThinking("", "", 0)
	if mode != thinkingModeFixed || effort != "" || budget < 1024 {
		t.Fatalf("expected fixed/empty/budget>=1024, got %q/%q/%d", mode, effort, budget)
	}

	mode, effort, budget = c.resolveThinking("off", "medium", 0)
	if mode != thinkingModeOff || effort != "" || budget != 0 {
		t.Fatalf("expected off/empty/0, got %q/%q/%d", mode, effort, budget)
	}

	c = &Client{thinkingMode: thinkingModeAdaptive, thinkingEffort: thinkingEffortHigh, thinkingBgt: 0}
	mode, effort, budget = c.resolveThinking("", "medium", 0)
	if mode != thinkingModeAdaptive || effort != thinkingEffortMedium || budget != 0 {
		t.Fatalf("expected adaptive/medium/0, got %q/%q/%d", mode, effort, budget)
	}

	mode, effort, budget = c.resolveThinking("fixed", "medium", 2048)
	if mode != thinkingModeFixed || effort != "" || budget != 2048 {
		t.Fatalf("expected fixed/empty/2048, got %q/%q/%d", mode, effort, budget)
	}
}

func TestApplyThinkingConfig(t *testing.T) {
	req := sdk.MessageNewParams{}
	applyThinkingConfig(&req, thinkingModeAdaptive, thinkingEffortMedium, 0)
	if req.Thinking.OfAdaptive == nil {
		t.Fatalf("expected adaptive thinking config")
	}
	if req.OutputConfig.Effort != sdk.OutputConfigEffortMedium {
		t.Fatalf("expected output_config effort %q, got %q", sdk.OutputConfigEffortMedium, req.OutputConfig.Effort)
	}

	req = sdk.MessageNewParams{}
	applyThinkingConfig(&req, thinkingModeOff, "", 0)
	if req.Thinking.OfDisabled == nil {
		t.Fatalf("expected disabled thinking config")
	}
	if req.OutputConfig.Effort != "" {
		t.Fatalf("expected empty output_config effort, got %q", req.OutputConfig.Effort)
	}

	req = sdk.MessageNewParams{}
	applyThinkingConfig(&req, thinkingModeFixed, "", 100)
	if req.Thinking.OfEnabled == nil {
		t.Fatalf("expected fixed thinking config")
	}
	if req.Thinking.OfEnabled.BudgetTokens < 1024 {
		t.Fatalf("expected minimum fixed budget of 1024, got %d", req.Thinking.OfEnabled.BudgetTokens)
	}
	if req.OutputConfig.Effort != "" {
		t.Fatalf("expected empty output_config effort for fixed mode, got %q", req.OutputConfig.Effort)
	}
}
