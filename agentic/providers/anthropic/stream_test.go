package anthropic

import (
	"encoding/json"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

func TestStreamTextDelta(t *testing.T) {
	state := newStreamState()

	event := sdk.ContentBlockDeltaEvent{
		Type:  constant.ContentBlockDelta("content_block_delta"),
		Index: 0,
		Delta: sdk.RawContentBlockDeltaUnion{
			Type: "text_delta",
			Text: "Hello",
		},
	}

	out := state.handle(mustUnion(t, event))
	if len(out) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out))
	}
	delta, ok := out[0].(TextDeltaEvent)
	if !ok {
		t.Fatalf("expected TextDeltaEvent, got %T", out[0])
	}
	if delta.Delta != "Hello" {
		t.Fatalf("expected delta %q, got %q", "Hello", delta.Delta)
	}
}

func TestStreamTextStart(t *testing.T) {
	state := newStreamState()

	event := sdk.ContentBlockStartEvent{
		Type:  constant.ContentBlockStart("content_block_start"),
		Index: 0,
		ContentBlock: sdk.ContentBlockStartEventContentBlockUnion{
			Type: "text",
			Text: "Hello",
		},
	}

	out := state.handle(mustUnion(t, event))
	if len(out) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out))
	}
	delta, ok := out[0].(TextDeltaEvent)
	if !ok {
		t.Fatalf("expected TextDeltaEvent, got %T", out[0])
	}
	if delta.Delta != "Hello" {
		t.Fatalf("expected delta %q, got %q", "Hello", delta.Delta)
	}
}

func TestStreamToolUseAssembly(t *testing.T) {
	state := newStreamState()

	start := sdk.ContentBlockStartEvent{
		Type:  constant.ContentBlockStart("content_block_start"),
		Index: 1,
		ContentBlock: sdk.ContentBlockStartEventContentBlockUnion{
			Type: "tool_use",
			ID:   "",
			Name: "add",
		},
	}
	state.handle(mustUnion(t, start))

	delta1 := sdk.ContentBlockDeltaEvent{
		Type:  constant.ContentBlockDelta("content_block_delta"),
		Index: 1,
		Delta: sdk.RawContentBlockDeltaUnion{
			Type:        "input_json_delta",
			PartialJSON: `{"a":10,`,
		},
	}
	state.handle(mustUnion(t, delta1))

	delta2 := sdk.ContentBlockDeltaEvent{
		Type:  constant.ContentBlockDelta("content_block_delta"),
		Index: 1,
		Delta: sdk.RawContentBlockDeltaUnion{
			Type:        "input_json_delta",
			PartialJSON: `"b":32}`,
		},
	}
	state.handle(mustUnion(t, delta2))

	stop := sdk.ContentBlockStopEvent{
		Type:  constant.ContentBlockStop("content_block_stop"),
		Index: 1,
	}
	out := state.handle(mustUnion(t, stop))

	if len(out) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out))
	}
	callEvent, ok := out[0].(ToolCallEvent)
	if !ok {
		t.Fatalf("expected ToolCallEvent, got %T", out[0])
	}
	if callEvent.Call.ID == "" {
		t.Fatalf("expected non-empty tool call ID")
	}
	if callEvent.Call.Name != "add" {
		t.Fatalf("expected tool name %q, got %q", "add", callEvent.Call.Name)
	}

	var args map[string]int
	if err := json.Unmarshal(callEvent.Call.Input, &args); err != nil {
		t.Fatalf("failed to parse tool input: %v", err)
	}
	if args["a"] != 10 || args["b"] != 32 {
		t.Fatalf("unexpected tool input: %#v", args)
	}
}

func TestStreamStopReasonMapping(t *testing.T) {
	state := newStreamState()

	event := sdk.MessageDeltaEvent{
		Type: constant.MessageDelta("message_delta"),
		Delta: sdk.MessageDeltaEventDelta{
			StopReason: sdk.StopReason("tool_use"),
		},
		Usage: sdk.MessageDeltaUsage{
			InputTokens:  3,
			OutputTokens: 5,
		},
	}

	state.handle(mustUnion(t, event))
	done := state.done()

	if done.StopReason != "tool_use" {
		t.Fatalf("expected stop reason %q, got %q", "tool_use", done.StopReason)
	}
	if done.StopReasonNormalized != usage.StopReasonTool {
		t.Fatalf("expected normalized stop reason %q, got %q", usage.StopReasonTool, done.StopReasonNormalized)
	}
	if done.Usage == nil || done.Usage.Total != 8 {
		t.Fatalf("expected usage total 8, got %#v", done.Usage)
	}
}

func mustUnion(t *testing.T, event any) sdk.MessageStreamEventUnion {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}
	var union sdk.MessageStreamEventUnion
	if err := json.Unmarshal(data, &union); err != nil {
		t.Fatalf("failed to unmarshal union: %v", err)
	}
	return union
}
