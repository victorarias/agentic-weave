package harness

import (
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/truncate"
)

func TestLoopTruncationHeadAndTail(t *testing.T) {
	output := json.RawMessage("one\ntwo\nthree")
	reg := agentic.NewRegistry()
	if err := reg.Register(staticTool{
		def: agentic.ToolDefinition{
			Name:           "echo",
			AllowedCallers: []string{"llm"},
		},
		output: output,
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	tests := []struct {
		name     string
		mode     truncate.Mode
		expected string
	}{
		{name: "head", mode: truncate.ModeHead, expected: "one\ntwo"},
		{name: "tail", mode: truncate.ModeTail, expected: "two\nthree"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decider := &scriptedDecider{
				script: []loop.Decision{
					{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
					{Reply: "done"},
				},
			}
			result, eventsSeen, err := runScenario(t, loop.Config{
				Decider:        decider,
				Executor:       reg,
				Truncation:     &truncate.Options{MaxLines: 2, MaxBytes: 100},
				TruncationMode: tt.mode,
			}, loop.Request{UserMessage: "hi"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.ToolResults) != 1 {
				t.Fatalf("expected 1 tool result")
			}
			if string(result.ToolResults[0].Output) != tt.expected {
				t.Fatalf("unexpected output: %q", string(result.ToolResults[0].Output))
			}
			found := false
			for _, event := range eventsSeen {
				if event.Type == events.ToolOutputTruncated {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected truncation event")
			}
		})
	}
}
