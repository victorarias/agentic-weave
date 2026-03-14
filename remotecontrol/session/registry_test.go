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
	if len(record.PendingPermissions) != 0 {
		t.Fatalf("expected disconnect to clear pending permissions: %#v", record)
	}
}

func TestRegistryTracksPendingPermissionsAndPhase(t *testing.T) {
	registry := NewRegistry()
	runtime := protocol.RuntimeInfo{ID: "rt-1", Kind: "pi", Transport: "rpc"}
	registry.Ensure("sess-1", "/tmp/sess-1.jsonl")
	registry.SetConnected("sess-1", runtime, "")

	permission := protocol.PermissionRequest{ID: "perm-1", Kind: "confirm", Title: "Need approval"}
	record, ok := registry.AddPermission("sess-1", "rt-1", permission)
	if !ok {
		t.Fatal("expected add permission to succeed")
	}
	if record.State != "waiting_permission" || len(record.PendingPermissions) != 1 {
		t.Fatalf("unexpected pending permission record: %#v", record)
	}
	if !registry.HasPendingPermission("sess-1", "perm-1") {
		t.Fatal("expected registry to report pending permission")
	}

	record, ok = registry.SetPhase("sess-1", "rt-1", "waiting_permission")
	if !ok {
		t.Fatal("expected set phase to succeed")
	}
	if record.Phase != "waiting_permission" || record.State != "waiting_permission" {
		t.Fatalf("unexpected waiting permission phase: %#v", record)
	}

	record, ok = registry.ResolvePermission("sess-1", "rt-1", "perm-1")
	if !ok {
		t.Fatal("expected resolve permission to succeed")
	}
	if len(record.PendingPermissions) != 0 {
		t.Fatalf("expected resolved permission to be removed: %#v", record)
	}
	if registry.HasPendingPermission("sess-1", "perm-1") {
		t.Fatal("expected resolved permission to be absent")
	}

	record, ok = registry.SetPhase("sess-1", "rt-1", "running")
	if !ok {
		t.Fatal("expected running phase update to succeed")
	}
	if record.State != "running" || record.Phase != "running" {
		t.Fatalf("expected running record after permission resolution: %#v", record)
	}
}

func TestRegistryTracksAttachment(t *testing.T) {
	registry := NewRegistry()
	registry.Ensure("sess-1", "/tmp/sess-1.jsonl")
	registry.SetConnected("sess-1", protocol.RuntimeInfo{ID: "rt-1", Kind: "pi", Transport: "rpc"}, "")

	record, ok := registry.SetAttachment("sess-1", protocol.AttachmentInfo{ClientID: "human-1", Mode: "observe"})
	if !ok {
		t.Fatal("expected set attachment to succeed")
	}
	if record.Attachment == nil || record.Attachment.ClientID != "human-1" || record.Attachment.Mode != "observe" {
		t.Fatalf("unexpected attachment record: %#v", record)
	}

	record, ok = registry.SetQueuedPrompts("sess-1", 2)
	if !ok {
		t.Fatal("expected queued prompt count to update")
	}
	if record.QueuedPrompts != 2 {
		t.Fatalf("expected queued prompt count to be tracked: %#v", record)
	}

	record, ok = registry.ClearAttachment("sess-1")
	if !ok {
		t.Fatal("expected clear attachment to succeed")
	}
	if record.Attachment != nil {
		t.Fatalf("expected attachment to be cleared: %#v", record)
	}
}

func TestRegistryConnectClearsStalePendingPermissions(t *testing.T) {
	registry := NewRegistry()
	registry.Ensure("sess-1", "/tmp/sess-1.jsonl")
	registry.SetConnected("sess-1", protocol.RuntimeInfo{ID: "rt-1", Kind: "pi", Transport: "rpc"}, "")
	registry.AddPermission("sess-1", "rt-1", protocol.PermissionRequest{ID: "perm-stale", Kind: "confirm"})
	registry.SetPhase("sess-1", "rt-1", "waiting_permission")

	record := registry.SetConnected("sess-1", protocol.RuntimeInfo{ID: "rt-2", Kind: "pi", Transport: "rpc"}, "")
	if record.Runtime.ID != "rt-2" {
		t.Fatalf("expected runtime swap to update runtime id: %#v", record)
	}
	if len(record.PendingPermissions) != 0 || record.State != "running" || record.Phase != "running" {
		t.Fatalf("expected new runtime to clear stale permission state: %#v", record)
	}
}

func TestRegistryReturnsDetachedCopies(t *testing.T) {
	registry := NewRegistry()
	registry.Ensure("sess-1", "/tmp/sess-1.jsonl")
	registry.SetConnected("sess-1", protocol.RuntimeInfo{ID: "rt-1", Kind: "pi", Transport: "rpc"}, "")
	registry.SetAttachment("sess-1", protocol.AttachmentInfo{ClientID: "human-1", Mode: "inject"})
	registry.AddPermission("sess-1", "rt-1", protocol.PermissionRequest{ID: "perm-1", Kind: "confirm", Options: []string{"allow"}, Raw: map[string]any{"a": "b"}})

	record, ok := registry.Get("sess-1")
	if !ok {
		t.Fatal("expected session record")
	}
	record.Attachment.ClientID = "mutated"
	record.PendingPermissions[0].ID = "mutated"
	record.PendingPermissions[0].Options[0] = "mutated"
	record.PendingPermissions[0].Raw["a"] = "mutated"

	list := registry.List()
	if len(list) != 1 {
		t.Fatalf("expected one listed session, got %d", len(list))
	}
	list[0].PendingPermissions[0].ID = "list-mutated"

	fresh, ok := registry.Get("sess-1")
	if !ok {
		t.Fatal("expected session record after mutations")
	}
	if fresh.Attachment == nil || fresh.Attachment.ClientID != "human-1" {
		t.Fatalf("expected attachment to remain unchanged: %#v", fresh)
	}
	if fresh.PendingPermissions[0].ID != "perm-1" || fresh.PendingPermissions[0].Options[0] != "allow" || fresh.PendingPermissions[0].Raw["a"] != "b" {
		t.Fatalf("expected permission data to remain unchanged: %#v", fresh.PendingPermissions[0])
	}
}
