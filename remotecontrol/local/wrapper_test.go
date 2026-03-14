package local

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

func TestWrapperPromptStreaming(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wrapper.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := NewWrapper(Config{
		SocketPath:      socket,
		SessionID:       "demo-session",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  10 * time.Second,
	})

	go func() {
		_ = wrapper.Run(ctx)
	}()
	waitForSocket(t, socket)

	conn := dialSocket(t, socket)
	defer conn.Close()
	reader := startReader(t, conn)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "init-1", protocol.InitializeCommand{
		Command:         protocol.CommandInitialize,
		ProtocolVersion: protocol.Version,
	}))
	awaitAck(t, reader, "init-1")
	awaitReady(t, reader)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "prompt-1", protocol.SessionPromptCommand{
		Command: protocol.CommandSessionPrompt,
		Message: "say hello",
	}))
	awaitAck(t, reader, "prompt-1")

	var deltaSeen bool
	var completeSeen bool
	deadline := time.After(10 * time.Second)
	for !(deltaSeen && completeSeen) {
		select {
		case env := <-reader:
			if env.Type != protocol.MessageEvent {
				continue
			}
			var meta struct {
				Event string `json:"event"`
			}
			if err := env.DecodePayload(&meta); err != nil {
				t.Fatalf("decode meta: %v", err)
			}
			if meta.Event != protocol.EventSessionUpdate {
				continue
			}
			var evt protocol.SessionUpdateEvent
			if err := env.DecodePayload(&evt); err != nil {
				t.Fatalf("decode update: %v", err)
			}
			switch evt.Update.Kind {
			case protocol.UpdateMessageDelta:
				if evt.Update.Delta != "hello" {
					t.Fatalf("unexpected delta: %#v", evt.Update)
				}
				deltaSeen = true
			case protocol.UpdateMessageComplete:
				if evt.Update.Message != "hello world" {
					t.Fatalf("unexpected message complete: %#v", evt.Update)
				}
			case protocol.UpdateComplete:
				completeSeen = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for streamed events")
		}
	}
}

func TestPromptToPICommandDeliveryModes(t *testing.T) {
	tests := []struct {
		name     string
		delivery string
		wantType string
	}{
		{name: "default", delivery: "", wantType: "prompt"},
		{name: "foreground", delivery: "foreground", wantType: "prompt"},
		{name: "interrupt", delivery: "interrupt", wantType: "steer"},
		{name: "steer", delivery: "steer", wantType: "steer"},
		{name: "queue", delivery: "queue", wantType: "follow_up"},
		{name: "deliver_when_idle", delivery: "deliver_when_idle", wantType: "follow_up"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := promptToPICommand(protocol.SessionPromptCommand{Message: "hello", Delivery: tc.delivery})
			if err != nil {
				t.Fatalf("promptToPICommand: %v", err)
			}
			if got := cmd["type"]; got != tc.wantType {
				t.Fatalf("unexpected type %v, want %s", got, tc.wantType)
			}
		})
	}
}

func TestWrapperIgnoresDuplicatePIResponses(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wrapper.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := NewWrapper(Config{
		SocketPath:      socket,
		SessionID:       "demo-session",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "duplicate_response"},
		NoDefaultPiArgs: true,
		StartupTimeout:  10 * time.Second,
	})

	go func() { _ = wrapper.Run(ctx) }()
	waitForSocket(t, socket)

	conn := dialSocket(t, socket)
	defer conn.Close()
	reader := startReader(t, conn)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "init-1", protocol.InitializeCommand{
		Command:         protocol.CommandInitialize,
		ProtocolVersion: protocol.Version,
	}))
	awaitAck(t, reader, "init-1")
	awaitReady(t, reader)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "prompt-1", protocol.SessionPromptCommand{
		Command: protocol.CommandSessionPrompt,
		Message: "say hello",
	}))
	awaitAck(t, reader, "prompt-1")

	deadline := time.After(10 * time.Second)
	for {
		select {
		case env := <-reader:
			if env.Type != protocol.MessageEvent {
				continue
			}
			var evt protocol.SessionUpdateEvent
			if err := env.DecodePayload(&evt); err != nil || evt.Event != protocol.EventSessionUpdate {
				continue
			}
			if evt.Update.Kind == protocol.UpdateMessageComplete {
				if evt.Update.Message != "hello world" {
					t.Fatalf("unexpected message complete: %#v", evt.Update)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for completion after duplicate response")
		}
	}
}

func TestWrapperStatus(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wrapper.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := NewWrapper(Config{
		SocketPath:      socket,
		SessionID:       "demo-session",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  10 * time.Second,
	})
	go func() {
		_ = wrapper.Run(ctx)
	}()
	waitForSocket(t, socket)

	conn := dialSocket(t, socket)
	defer conn.Close()
	reader := startReader(t, conn)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "init-1", protocol.InitializeCommand{
		Command:         protocol.CommandInitialize,
		ProtocolVersion: protocol.Version,
	}))
	awaitAck(t, reader, "init-1")
	awaitReady(t, reader)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "status-1", protocol.SessionStatusCommand{
		Command: protocol.CommandSessionStatus,
	}))
	ack := awaitAckEnvelope(t, reader, "status-1")
	var payload protocol.AckPayload
	if err := ack.DecodePayload(&payload); err != nil {
		t.Fatalf("decode status ack: %v", err)
	}
	sessionMap, _ := payload.Data["session"].(map[string]any)
	runtimeMap, _ := payload.Data["runtime"].(map[string]any)
	if sessionMap["id"] != "demo-session" {
		t.Fatalf("unexpected session status: %#v", payload.Data)
	}
	if runtimeMap["id"] == "" {
		t.Fatalf("expected runtime id in status: %#v", payload.Data)
	}
}

func TestWrapperPermissionFlow(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wrapper.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := NewWrapper(Config{
		SocketPath:      socket,
		SessionID:       "demo-session",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "permission"},
		NoDefaultPiArgs: true,
		StartupTimeout:  10 * time.Second,
	})
	go func() { _ = wrapper.Run(ctx) }()
	waitForSocket(t, socket)

	conn := dialSocket(t, socket)
	defer conn.Close()
	reader := startReader(t, conn)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version}))
	awaitAck(t, reader, "init-1")
	awaitReady(t, reader)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "need approval"}))
	awaitAck(t, reader, "prompt-1")
	awaitPermissionRequest(t, reader, "perm-1")

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "allow-1", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: "perm-1", Decision: "allow"}))
	awaitAck(t, reader, "allow-1")
	awaitPermissionResolved(t, reader, "perm-1", "allow")

	deadline := time.After(10 * time.Second)
	for {
		select {
		case env := <-reader:
			if env.Type != protocol.MessageEvent {
				continue
			}
			var evt protocol.SessionUpdateEvent
			if err := env.DecodePayload(&evt); err != nil {
				continue
			}
			if evt.Event == protocol.EventSessionUpdate && evt.Update.Kind == protocol.UpdateComplete {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for completion after permission allow")
		}
	}
}

func TestWrapperStatusShowsPendingPermission(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wrapper.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := NewWrapper(Config{
		SocketPath:      socket,
		SessionID:       "demo-session",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "permission"},
		NoDefaultPiArgs: true,
		StartupTimeout:  10 * time.Second,
	})
	go func() { _ = wrapper.Run(ctx) }()
	waitForSocket(t, socket)

	conn := dialSocket(t, socket)
	defer conn.Close()
	reader := startReader(t, conn)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version}))
	awaitAck(t, reader, "init-1")
	awaitReady(t, reader)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "need approval"}))
	awaitAck(t, reader, "prompt-1")
	awaitPermissionRequest(t, reader, "perm-1")
	awaitStatusPhase(t, reader, "waiting_permission")

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "status-1", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus}))
	ack := awaitAckEnvelope(t, reader, "status-1")
	var payload protocol.AckPayload
	if err := ack.DecodePayload(&payload); err != nil {
		t.Fatalf("decode status ack: %v", err)
	}
	if payload.Data["state"] != "waiting_permission" {
		t.Fatalf("expected waiting_permission state: %#v", payload.Data)
	}
	if payload.Data["phase"] != "waiting_permission" {
		t.Fatalf("expected waiting_permission phase: %#v", payload.Data)
	}
	pending, _ := payload.Data["pending_permissions"].([]any)
	if len(pending) != 1 {
		t.Fatalf("expected one pending permission: %#v", payload.Data)
	}
	first, _ := pending[0].(map[string]any)
	if first["id"] != "perm-1" || first["kind"] != "confirm" {
		t.Fatalf("unexpected pending permission: %#v", first)
	}
}

func TestWrapperPermissionInvalidDecisionDoesNotDropRequest(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wrapper.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := NewWrapper(Config{
		SocketPath:      socket,
		SessionID:       "demo-session",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "permission"},
		NoDefaultPiArgs: true,
		StartupTimeout:  10 * time.Second,
	})
	go func() { _ = wrapper.Run(ctx) }()
	waitForSocket(t, socket)

	conn := dialSocket(t, socket)
	defer conn.Close()
	reader := startReader(t, conn)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version}))
	awaitAck(t, reader, "init-1")
	awaitReady(t, reader)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "need approval"}))
	awaitAck(t, reader, "prompt-1")
	awaitPermissionRequest(t, reader, "perm-1")

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "maybe-1", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: "perm-1", Decision: "maybe"}))
	awaitErrorEnvelope(t, reader, "maybe-1", `unsupported permission decision "maybe"`)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "allow-1", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: "perm-1", Decision: "allow"}))
	awaitAck(t, reader, "allow-1")
	awaitPermissionResolved(t, reader, "perm-1", "allow")
}

func TestWrapperCancel(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wrapper.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := NewWrapper(Config{
		SocketPath:      socket,
		SessionID:       "demo-session",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "abortable"},
		NoDefaultPiArgs: true,
		StartupTimeout:  10 * time.Second,
	})
	go func() {
		_ = wrapper.Run(ctx)
	}()
	waitForSocket(t, socket)

	conn := dialSocket(t, socket)
	defer conn.Close()
	reader := startReader(t, conn)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "init-1", protocol.InitializeCommand{
		Command:         protocol.CommandInitialize,
		ProtocolVersion: protocol.Version,
	}))
	awaitAck(t, reader, "init-1")
	awaitReady(t, reader)

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "prompt-1", protocol.SessionPromptCommand{
		Command: protocol.CommandSessionPrompt,
		Message: "long run",
	}))
	awaitAck(t, reader, "prompt-1")

	sendEnvelope(t, conn, mustEnvelope(t, protocol.MessageCommand, "demo-session", "", "client", "cancel-1", protocol.SessionCancelCommand{
		Command: protocol.CommandSessionCancel,
	}))
	deadline := time.After(10 * time.Second)
	seenAck := false
	seenComplete := false
	for !seenAck || !seenComplete {
		select {
		case env := <-reader:
			if env.Type == protocol.MessageAck && env.ID == "cancel-1" {
				seenAck = true
				continue
			}
			if env.Type != protocol.MessageEvent {
				continue
			}
			var evt protocol.SessionUpdateEvent
			if err := env.DecodePayload(&evt); err != nil {
				continue
			}
			if evt.Event == protocol.EventSessionUpdate && evt.Update.Kind == protocol.UpdateComplete {
				seenComplete = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for cancel ack and completion")
		}
	}
}

func TestFakePIProcess(t *testing.T) {
	if os.Getenv("WEAVE_FAKE_PI") != "1" {
		return
	}
	scenario := os.Getenv("WEAVE_FAKE_PI_SCENARIO")
	if err := runFakePI(scenario); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

func runFakePI(scenario string) error {
	aborted := make(chan struct{})
	permissionResolved := make(chan bool, 1)
	var writeMu sync.Mutex
	write := func(payload map[string]any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return protocol.WriteJSONLine(os.Stdout, payload)
	}
	return protocol.ReadJSONL(os.Stdin, func(line []byte) error {
		var cmd map[string]any
		if err := json.Unmarshal(line, &cmd); err != nil {
			return err
		}
		switch cmd["type"] {
		case "get_state":
			return write(map[string]any{
				"id":      cmd["id"],
				"type":    "response",
				"command": "get_state",
				"success": true,
				"data":    map[string]any{"isStreaming": false, "sessionId": "fake-session"},
			})
		case "prompt", "steer", "follow_up":
			response := map[string]any{
				"id":      cmd["id"],
				"type":    "response",
				"command": cmd["type"],
				"success": true,
			}
			if err := write(response); err != nil {
				return err
			}
			if scenario == "duplicate_response" {
				if err := write(response); err != nil {
					return err
				}
			}
			go func() {
				_ = write(map[string]any{"type": "agent_start"})
				if scenario == "abortable" {
					<-aborted
					_ = write(map[string]any{"type": "agent_end", "messages": []any{}})
					return
				}
				if scenario == "permission" {
					time.Sleep(20 * time.Millisecond)
					_ = write(map[string]any{"type": "extension_ui_request", "id": "perm-1", "method": "confirm", "title": "Allow file write?", "message": "Need approval"})
					allowed := <-permissionResolved
					if !allowed {
						_ = write(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "error", "reason": "permission denied"}, "message": map[string]any{"role": "assistant"}})
						_ = write(map[string]any{"type": "agent_end", "messages": []any{}})
						return
					}
				}
				time.Sleep(20 * time.Millisecond)
				_ = write(map[string]any{
					"type":                  "message_update",
					"assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "hello"},
					"message":               map[string]any{"role": "assistant"},
				})
				time.Sleep(20 * time.Millisecond)
				_ = write(map[string]any{
					"type": "message_end",
					"message": map[string]any{
						"role": "assistant",
						"content": []any{
							map[string]any{"type": "text", "text": "hello world"},
						},
					},
				})
				time.Sleep(20 * time.Millisecond)
				_ = write(map[string]any{"type": "agent_end", "messages": []any{}})
			}()
			return nil
		case "abort":
			if err := write(map[string]any{
				"id":      cmd["id"],
				"type":    "response",
				"command": "abort",
				"success": true,
			}); err != nil {
				return err
			}
			closeOnce(aborted)
			return nil
		case "extension_ui_response":
			if cmd["id"] == "perm-1" {
				confirmed, _ := cmd["confirmed"].(bool)
				permissionResolved <- confirmed
				return nil
			}
			return nil
		default:
			return write(map[string]any{
				"id":      cmd["id"],
				"type":    "response",
				"command": cmd["type"],
				"success": false,
				"error":   fmt.Sprintf("unknown command: %v", cmd["type"]),
			})
		}
	})
}

func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func helperProcessPath(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return exe
}

func dialSocket(t *testing.T, socket string) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	return conn
}

func waitForSocket(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear", socket)
}

func startReader(t *testing.T, conn net.Conn) <-chan protocol.Envelope {
	t.Helper()
	ch := make(chan protocol.Envelope, 64)
	go func() {
		defer close(ch)
		_ = protocol.ReadJSONL(conn, func(line []byte) error {
			env, err := protocol.DecodeEnvelope(line)
			if err != nil {
				return err
			}
			ch <- env
			return nil
		})
	}()
	return ch
}

func mustEnvelope(t *testing.T, typ, sessionID, runtimeID, from, id string, payload any) protocol.Envelope {
	t.Helper()
	env, err := protocol.NewEnvelope(typ, sessionID, runtimeID, from, id, payload)
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	return env
}

func sendEnvelope(t *testing.T, conn net.Conn, env protocol.Envelope) {
	t.Helper()
	if err := protocol.WriteJSONLine(conn, env); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
}

func awaitAck(t *testing.T, ch <-chan protocol.Envelope, id string) {
	t.Helper()
	_ = awaitAckEnvelope(t, ch, id)
}

func awaitAckEnvelope(t *testing.T, ch <-chan protocol.Envelope, id string) protocol.Envelope {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case env := <-ch:
			if env.ID != id || env.Type != protocol.MessageAck {
				continue
			}
			return env
		case <-deadline:
			t.Fatalf("timed out waiting for ack %s", id)
		}
	}
}

func awaitErrorEnvelope(t *testing.T, ch <-chan protocol.Envelope, id, want string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case env := <-ch:
			if env.ID != id || env.Type != protocol.MessageError {
				continue
			}
			var payload protocol.ErrorPayload
			if err := env.DecodePayload(&payload); err != nil {
				t.Fatalf("decode error payload: %v", err)
			}
			if payload.Error != want {
				t.Fatalf("unexpected error %q, want %q", payload.Error, want)
			}
			return
		case <-deadline:
			t.Fatalf("timed out waiting for error %s", id)
		}
	}
}

func awaitPermissionRequest(t *testing.T, ch <-chan protocol.Envelope, requestID string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case env := <-ch:
			if env.Type != protocol.MessageEvent {
				continue
			}
			var evt protocol.SessionUpdateEvent
			if err := env.DecodePayload(&evt); err != nil {
				continue
			}
			if evt.Event == protocol.EventSessionUpdate && evt.Update.Kind == protocol.UpdatePermissionRequest && evt.Update.RequestID == requestID {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for permission request %s", requestID)
		}
	}
}

func awaitStatusPhase(t *testing.T, ch <-chan protocol.Envelope, phase string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case env := <-ch:
			if env.Type != protocol.MessageEvent {
				continue
			}
			var evt protocol.SessionUpdateEvent
			if err := env.DecodePayload(&evt); err != nil {
				continue
			}
			if evt.Event == protocol.EventSessionUpdate && evt.Update.Kind == protocol.UpdateStatus && evt.Update.Phase == phase {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for status phase %s", phase)
		}
	}
}

func awaitPermissionResolved(t *testing.T, ch <-chan protocol.Envelope, requestID, decision string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case env := <-ch:
			if env.Type != protocol.MessageEvent {
				continue
			}
			var evt protocol.SessionUpdateEvent
			if err := env.DecodePayload(&evt); err != nil {
				continue
			}
			if evt.Event == protocol.EventSessionUpdate && evt.Update.Kind == protocol.UpdatePermissionResolved && evt.Update.RequestID == requestID && evt.Update.Decision == decision {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for permission resolved %s", requestID)
		}
	}
}

func awaitReady(t *testing.T, ch <-chan protocol.Envelope) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case env := <-ch:
			if env.Type != protocol.MessageEvent {
				continue
			}
			var evt protocol.AgentReadyEvent
			if err := env.DecodePayload(&evt); err != nil {
				continue
			}
			if evt.Event == protocol.EventSessionAgentReady {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for agent_ready")
		}
	}
}
