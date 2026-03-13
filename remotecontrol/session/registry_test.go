package session

import (
	"testing"

	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

func TestRegistryTracksConnectedAndDisconnectedRuntime(t *testing.T) {
	registry := NewRegistry()
	runtime := protocol.RuntimeInfo{ID: "rt-1", Kind: "pi", Transport: "rpc"}

	registry.SetConnected("sess-1", runtime)
	record, ok := registry.Get("sess-1")
	if !ok {
		t.Fatal("expected session record")
	}
	if record.Session.ID != "sess-1" || record.Runtime.ID != "rt-1" || !record.WrapperConnected {
		t.Fatalf("unexpected connected record: %#v", record)
	}

	record, ok = registry.SetDisconnected("sess-1", "rt-1")
	if !ok {
		t.Fatal("expected disconnect to update record")
	}
	if record.WrapperConnected {
		t.Fatalf("expected disconnected record: %#v", record)
	}
}
