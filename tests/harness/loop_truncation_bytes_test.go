package harness

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/truncate"
)

func TestLoopTruncationByteLimitHead(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(staticTool{
		def: agentic.ToolDefinition{
			Name:           "echo",
			AllowedCallers: []string{"llm"},
		},
		output: json.RawMessage("abcdefghij\nsecond"),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
			{Reply: "done"},
		},
	}

	result, eventsSeen, err := runScenario(t, loop.Config{
		Decider:        decider,
		Executor:       reg,
		Truncation:     &truncate.Options{MaxLines: 10, MaxBytes: 4},
		TruncationMode: truncate.ModeHead,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.ToolResults[0].Output) != "abcd" {
		t.Fatalf("unexpected output: %q", string(result.ToolResults[0].Output))
	}
	assertTruncationBytesEvent(t, eventsSeen)
}

func TestLoopTruncationByteLimitTail(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(staticTool{
		def: agentic.ToolDefinition{
			Name:           "echo",
			AllowedCallers: []string{"llm"},
		},
		output: json.RawMessage("line1\nline2\nline3"),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	decider := &scriptedDecider{
		script: []loop.Decision{
			{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: json.RawMessage(`{}`)}}},
			{Reply: "done"},
		},
	}

	result, eventsSeen, err := runScenario(t, loop.Config{
		Decider:        decider,
		Executor:       reg,
		Truncation:     &truncate.Options{MaxLines: 10, MaxBytes: 2},
		TruncationMode: truncate.ModeTail,
	}, loop.Request{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.ToolResults[0].Output) != "e3" {
		t.Fatalf("unexpected output: %q", string(result.ToolResults[0].Output))
	}
	assertTruncationBytesEvent(t, eventsSeen)
}

func assertTruncationBytesEvent(t *testing.T, eventsSeen []events.Event) {
	t.Helper()
	for _, event := range eventsSeen {
		if event.Type == events.ToolOutputTruncated {
			if !strings.Contains(event.Content, "bytes") {
				t.Fatalf("expected truncation bytes content, got %q", event.Content)
			}
			return
		}
	}
	t.Fatalf("expected truncation event")
}
