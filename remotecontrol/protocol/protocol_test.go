package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	env, err := NewEnvelope(MessageCommand, "sess-1", "rt-1", "tester", "msg-1", InitializeCommand{
		Command:         CommandInitialize,
		ProtocolVersion: Version,
		Capabilities:    map[string]bool{"session_prompt": true},
	})
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	decoded, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if decoded.Type != MessageCommand {
		t.Fatalf("unexpected type: %q", decoded.Type)
	}

	var init InitializeCommand
	if err := decoded.DecodePayload(&init); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if init.Command != CommandInitialize {
		t.Fatalf("unexpected command: %q", init.Command)
	}
	if !init.Capabilities["session_prompt"] {
		t.Fatal("expected capability to survive round trip")
	}
}

func TestReadAndWriteJSONL(t *testing.T) {
	var buf bytes.Buffer
	first, err := NewEnvelope(MessageEvent, "sess-1", "rt-1", "wrapper", "", SessionUpdateEvent{
		Event:  EventSessionUpdate,
		Update: SessionUpdate{Kind: UpdateLifecycle, Phase: "running"},
	})
	if err != nil {
		t.Fatalf("first envelope: %v", err)
	}
	second, err := NewEnvelope(MessageAck, "sess-1", "rt-1", "wrapper", "cmd-1", AckPayload{
		Command: CommandSessionPrompt,
		Success: true,
	})
	if err != nil {
		t.Fatalf("second envelope: %v", err)
	}
	if err := WriteJSONLine(&buf, first); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := WriteJSONLine(&buf, second); err != nil {
		t.Fatalf("write second: %v", err)
	}

	var got []Envelope
	if err := ReadJSONL(&buf, func(line []byte) error {
		env, err := DecodeEnvelope(line)
		if err != nil {
			return err
		}
		got = append(got, env)
		return nil
	}); err != nil {
		t.Fatalf("read jsonl: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 envelopes, got %d", len(got))
	}
	if got[0].Type != MessageEvent || got[1].Type != MessageAck {
		t.Fatalf("unexpected envelope ordering: %#v", got)
	}
}

func TestReadJSONLRejectsOversizedLine(t *testing.T) {
	line := append(bytes.Repeat([]byte("a"), MaxJSONLLineBytes+1), '\n')
	if err := ReadJSONL(bytes.NewReader(line), func([]byte) error { return nil }); err == nil {
		t.Fatal("expected oversized jsonl line to fail")
	}
}
