package harness

import (
	"testing"

	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

func TestLoopUsageAndStopReasonPassthrough(t *testing.T) {
	expected := &usage.Usage{Input: 1, Output: 2, Total: 3}
	decider := &scriptedDecider{
		script: []loop.Decision{{
			Reply:      "ok",
			Usage:      expected,
			StopReason: usage.StopReasonMaxTokens,
		}},
	}

	result, _, err := runScenario(t, loop.Config{
		Decider: decider,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Usage == nil {
		t.Fatalf("expected usage")
	}
	if *result.Usage != *expected {
		t.Fatalf("unexpected usage: %#v", result.Usage)
	}
	if result.StopReason != usage.StopReasonMaxTokens {
		t.Fatalf("unexpected stop reason: %q", result.StopReason)
	}
}
