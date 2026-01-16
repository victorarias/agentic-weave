package harness

import (
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/truncate"
)

func TestLoopEventOrderForToolTruncation(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(staticTool{
		def: agentic.ToolDefinition{
			Name:           "echo",
			AllowedCallers: []string{"llm"},
		},
		output: json.RawMessage("one\ntwo\nthree"),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
			{Reply: "done"},
		},
	}

	_, eventsSeen, err := runScenario(t, loop.Config{
		Decider:        decider,
		Executor:       reg,
		Truncation:     &truncate.Options{MaxLines: 1, MaxBytes: 100},
		TruncationMode: truncate.ModeTail,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	startIdx := -1
	truncIdx := -1
	endIdx := -1
	for i, event := range eventsSeen {
		switch event.Type {
		case events.ToolStart:
			if startIdx == -1 {
				startIdx = i
			}
		case events.ToolOutputTruncated:
			if truncIdx == -1 {
				truncIdx = i
			}
		case events.ToolEnd:
			if endIdx == -1 {
				endIdx = i
			}
		}
	}

	if startIdx == -1 || truncIdx == -1 || endIdx == -1 {
		t.Fatalf("missing tool events (start=%d, trunc=%d, end=%d)", startIdx, truncIdx, endIdx)
	}
	if !(startIdx < truncIdx && truncIdx < endIdx) {
		t.Fatalf("unexpected tool event order: start=%d trunc=%d end=%d", startIdx, truncIdx, endIdx)
	}
}
