package session

import (
	"testing"

	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

func TestRegistryTracksConnectedAndDisconnectedRuntime(t *testing.T) {
	registry := NewRegistry()
	runtime := protocol.RuntimeInfo{ID: "rt-1", Kind: "pi", Transport: "rpc"}

	registry.Ensure("sess-1", "/tmp/sess-1.jsonl")
	registry.SetConnected("sess-1", runtime, "")
	record, ok := registry.Get("sess-1")
	if !ok {
		t.Fatal("expected session record")
	}
	if record.Session.ID != "sess-1" || record.Runtime.ID != "rt-1" || !record.WrapperConnected || record.State != "running" {
		t.Fatalf("unexpected connected record: %#v", record)
	}
	if record.PersistedSessionHandle != "/tmp/sess-1.jsonl" {
		t.Fatalf("expected persisted handle, got %#v", record)
	}

	record, ok = registry.SetDisconnected("sess-1", "rt-1")
	if !ok {
		t.Fatal("expected disconnect to update record")
	}
	if record.WrapperConnected || record.State != "stopped" {
		t.Fatalf("expected disconnected record: %#v", record)
	}
}
