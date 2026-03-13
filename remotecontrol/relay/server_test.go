package relay

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"net/http/httptest"

	"github.com/gorilla/websocket"
	"github.com/victorarias/agentic-weave/remotecontrol/local"
	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

func TestRelayRoutesWrapperEventsToClient(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-1",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() {
		_ = wrapper.RunRelay(ctx, relayURL, "secret")
	}()
	waitForWrapperRegistration(t, srv, "sess-1")

	clientConn := dialWS(t, relayURL)
	defer clientConn.Close()
	authenticateWS(t, clientConn, protocol.RoleClient, "secret", "")

	initEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-1", "", "test-client", "init-1", protocol.InitializeCommand{
		Command:         protocol.CommandInitialize,
		ProtocolVersion: protocol.Version,
	})
	if err != nil {
		t.Fatalf("new init envelope: %v", err)
	}
	if err := clientConn.WriteJSON(initEnv); err != nil {
		t.Fatalf("write init: %v", err)
	}
	awaitWSAck(t, clientConn, "init-1")
	awaitReadyEvent(t, clientConn)

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-1", "", "test-client", "prompt-1", protocol.SessionPromptCommand{
		Command: protocol.CommandSessionPrompt,
		Message: "say hello",
	})
	if err != nil {
		t.Fatalf("new prompt envelope: %v", err)
	}
	if err := clientConn.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	awaitWSAck(t, clientConn, "prompt-1")
	awaitMessageComplete(t, clientConn, "hello world")
}

func TestRelayRejectsInvalidToken(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	conn := dialWS(t, relayURL)
	defer conn.Close()
	auth, err := protocol.NewEnvelope(protocol.MessageCommand, "", "", "test-client", "auth-1", protocol.AuthCommand{
		Command: protocol.CommandAuth,
		Token:   "wrong",
		Role:    protocol.RoleClient,
	})
	if err != nil {
		t.Fatalf("new auth envelope: %v", err)
	}
	if err := conn.WriteJSON(auth); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	var env protocol.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if env.Type != protocol.MessageError {
		t.Fatalf("expected error, got %#v", env)
	}
}

func TestFakePIProcess(t *testing.T) {
	if os.Getenv("WEAVE_FAKE_PI") != "1" {
		return
	}
	scenario := os.Getenv("WEAVE_FAKE_PI_SCENARIO")
	if err := runFakePI(scenario); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func helperProcessPath(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return exe
}

func runFakePI(scenario string) error {
	aborted := make(chan struct{})
	return protocol.ReadJSONL(os.Stdin, func(line []byte) error {
		var cmd map[string]any
		if err := json.Unmarshal(line, &cmd); err != nil {
			return err
		}
		switch cmd["type"] {
		case "get_state":
			return protocol.WriteJSONLine(os.Stdout, map[string]any{"id": cmd["id"], "type": "response", "command": "get_state", "success": true, "data": map[string]any{"isStreaming": false}})
		case "prompt", "steer", "follow_up":
			if err := protocol.WriteJSONLine(os.Stdout, map[string]any{"id": cmd["id"], "type": "response", "command": cmd["type"], "success": true}); err != nil {
				return err
			}
			go func() {
				_ = protocol.WriteJSONLine(os.Stdout, map[string]any{"type": "agent_start"})
				if scenario == "abortable" {
					<-aborted
					_ = protocol.WriteJSONLine(os.Stdout, map[string]any{"type": "agent_end", "messages": []any{}})
					return
				}
				time.Sleep(20 * time.Millisecond)
				_ = protocol.WriteJSONLine(os.Stdout, map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "hello"}, "message": map[string]any{"role": "assistant"}})
				time.Sleep(20 * time.Millisecond)
				_ = protocol.WriteJSONLine(os.Stdout, map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "hello world"}}}})
				time.Sleep(20 * time.Millisecond)
				_ = protocol.WriteJSONLine(os.Stdout, map[string]any{"type": "agent_end", "messages": []any{}})
			}()
			return nil
		case "abort":
			closeOnce(aborted)
			return protocol.WriteJSONLine(os.Stdout, map[string]any{"id": cmd["id"], "type": "response", "command": "abort", "success": true})
		default:
			return protocol.WriteJSONLine(os.Stdout, map[string]any{"id": cmd["id"], "type": "response", "command": cmd["type"], "success": false, "error": "unknown command"})
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

func waitForWrapperRegistration(t *testing.T, srv *Server, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		_, ok := srv.wrappers[sessionID]
		srv.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("wrapper for session %s did not register", sessionID)
}

func dialWS(t *testing.T, relayURL string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(relayURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn
}

func authenticateWS(t *testing.T, conn *websocket.Conn, role, token, sessionID string) {
	t.Helper()
	env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "test", "auth-1", protocol.AuthCommand{
		Command: protocol.CommandAuth,
		Token:   token,
		Role:    role,
	})
	if err != nil {
		t.Fatalf("new auth envelope: %v", err)
	}
	if err := conn.WriteJSON(env); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	awaitWSAck(t, conn, "auth-1")
}

func awaitWSAck(t *testing.T, conn *websocket.Conn, id string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("read json: %v", err)
		}
		if env.ID != id {
			continue
		}
		if env.Type == protocol.MessageAck {
			return
		}
		if env.Type == protocol.MessageError {
			var payload protocol.ErrorPayload
			if err := env.DecodePayload(&payload); err != nil {
				t.Fatalf("decode error payload: %v", err)
			}
			t.Fatalf("received error for %s: %s", id, payload.Error)
		}
	}
}

func awaitReadyEvent(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("read ready event: %v", err)
		}
		if env.Type != protocol.MessageEvent {
			continue
		}
		var ready protocol.AgentReadyEvent
		if err := env.DecodePayload(&ready); err == nil && ready.Event == protocol.EventSessionAgentReady {
			return
		}
	}
}

func awaitMessageComplete(t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("read message complete: %v", err)
		}
		if env.Type != protocol.MessageEvent {
			continue
		}
		var evt protocol.SessionUpdateEvent
		if err := env.DecodePayload(&evt); err != nil || evt.Event != protocol.EventSessionUpdate {
			continue
		}
		if evt.Update.Kind == protocol.UpdateMessageComplete {
			if evt.Update.Message != want {
				t.Fatalf("unexpected complete message %q", evt.Update.Message)
			}
			return
		}
	}
}
