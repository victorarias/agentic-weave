package main

import (
	"testing"

	"github.com/victorarias/agentic-weave/agentic/usage"
)

func TestNormalizeStopReason(t *testing.T) {
	cases := map[string]usage.StopReason{
		"tool_use":   usage.StopReasonTool,
		"max_tokens": usage.StopReasonMaxTokens,
		"end_turn":   usage.StopReasonStop,
		"stop":       usage.StopReasonStop,
		"unknown":    usage.StopReasonStop,
	}
	for in, want := range cases {
		if got := normalizeStopReason(in); got != want {
			t.Fatalf("normalizeStopReason(%q)=%v want %v", in, got, want)
		}
	}
}
