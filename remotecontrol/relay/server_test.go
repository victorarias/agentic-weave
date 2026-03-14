package relay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestRelaySessionStatusUsesRegistry(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-status",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() {
		_ = wrapper.RunRelay(ctx, relayURL, "secret")
	}()
	waitForWrapperRegistration(t, srv, "sess-status")

	clientConn := dialWS(t, relayURL)
	defer clientConn.Close()
	authenticateWS(t, clientConn, protocol.RoleClient, "secret", "")

	statusEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-status", "", "test-client", "status-1", protocol.SessionStatusCommand{
		Command: protocol.CommandSessionStatus,
	})
	if err != nil {
		t.Fatalf("new status envelope: %v", err)
	}
	if err := clientConn.WriteJSON(statusEnv); err != nil {
		t.Fatalf("write status: %v", err)
	}
	ack := awaitWSAckEnvelope(t, clientConn, "status-1")
	var payload protocol.AckPayload
	if err := ack.DecodePayload(&payload); err != nil {
		t.Fatalf("decode status ack: %v", err)
	}
	sessionMap, _ := payload.Data["session"].(map[string]any)
	runtimeMap, _ := payload.Data["runtime"].(map[string]any)
	if sessionMap["id"] != "sess-status" {
		t.Fatalf("unexpected session in payload: %#v", payload.Data)
	}
	if runtimeMap["id"] == "" {
		t.Fatalf("expected runtime id in payload: %#v", payload.Data)
	}
	if state, _ := payload.Data["state"].(string); state != "running" {
		t.Fatalf("expected running state: %#v", payload.Data)
	}
	if connected, _ := payload.Data["wrapper_connected"].(bool); !connected {
		t.Fatalf("expected wrapper_connected=true: %#v", payload.Data)
	}
}

func TestRelayObserveAttachConflict(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-observe",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() { _ = wrapper.RunRelay(ctx, relayURL, "secret") }()
	waitForWrapperRegistration(t, srv, "sess-observe")

	human1 := dialWS(t, relayURL)
	defer human1.Close()
	authenticateWSAs(t, human1, protocol.RoleClient, "secret", "", "human-1")
	initEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-observe", "", "human-1", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new init envelope: %v", err)
	}
	if err := human1.WriteJSON(initEnv); err != nil {
		t.Fatalf("write init: %v", err)
	}
	awaitWSAck(t, human1, "init-1")
	awaitReadyEvent(t, human1)
	attachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-observe", "", "human-1", "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "observe"})
	if err != nil {
		t.Fatalf("new attach envelope: %v", err)
	}
	if err := human1.WriteJSON(attachEnv); err != nil {
		t.Fatalf("write attach: %v", err)
	}
	ack := awaitWSAckEnvelope(t, human1, "attach-1")
	payload := decodeAckPayload(t, ack)
	attachment, _ := payload.Data["attachment"].(map[string]any)
	if attachment["client_id"] != "human-1" || attachment["mode"] != "observe" {
		t.Fatalf("unexpected attachment payload: %#v", payload.Data)
	}

	human2 := dialWS(t, relayURL)
	defer human2.Close()
	authenticateWSAs(t, human2, protocol.RoleClient, "secret", "", "human-2")
	initEnv2, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-observe", "", "human-2", "init-2", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new second init envelope: %v", err)
	}
	if err := human2.WriteJSON(initEnv2); err != nil {
		t.Fatalf("write second init: %v", err)
	}
	awaitWSAck(t, human2, "init-2")
	awaitReadyEvent(t, human2)
	attachEnv2, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-observe", "", "human-2", "attach-2", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "observe"})
	if err != nil {
		t.Fatalf("new second attach envelope: %v", err)
	}
	if err := human2.WriteJSON(attachEnv2); err != nil {
		t.Fatalf("write second attach: %v", err)
	}
	awaitWSError(t, human2, "attach-2", "another controller is already attached")
}

func TestRelaySameIdentityCanEscalateObserveToInject(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-escalate",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() { _ = wrapper.RunRelay(ctx, relayURL, "secret") }()
	waitForWrapperRegistration(t, srv, "sess-escalate")

	orch := dialWS(t, relayURL)
	defer orch.Close()
	authenticateWSAs(t, orch, protocol.RoleClient, "secret", "", "orch-1")
	orchInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-escalate", "", "orch-1", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new orch init: %v", err)
	}
	if err := orch.WriteJSON(orchInit); err != nil {
		t.Fatalf("write orch init: %v", err)
	}
	awaitWSAck(t, orch, "init-1")
	awaitReadyEvent(t, orch)

	observeConn := dialWS(t, relayURL)
	defer observeConn.Close()
	authenticateWSAs(t, observeConn, protocol.RoleClient, "secret", "", "human-1")
	observeInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-escalate", "", "human-1", "init-2", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new observe init: %v", err)
	}
	if err := observeConn.WriteJSON(observeInit); err != nil {
		t.Fatalf("write observe init: %v", err)
	}
	awaitWSAck(t, observeConn, "init-2")
	awaitReadyEvent(t, observeConn)
	attachObserve, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-escalate", "", "human-1", "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "observe"})
	if err != nil {
		t.Fatalf("new attach observe: %v", err)
	}
	if err := observeConn.WriteJSON(attachObserve); err != nil {
		t.Fatalf("write attach observe: %v", err)
	}
	awaitWSAck(t, observeConn, "attach-1")

	blockedPrompt, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-escalate", "", "human-1", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "say hello"})
	if err != nil {
		t.Fatalf("new blocked prompt: %v", err)
	}
	if err := observeConn.WriteJSON(blockedPrompt); err != nil {
		t.Fatalf("write blocked prompt: %v", err)
	}
	awaitWSError(t, observeConn, "prompt-1", "attached client in observe mode cannot send prompts")

	injectConn := dialWS(t, relayURL)
	defer injectConn.Close()
	authenticateWSAs(t, injectConn, protocol.RoleClient, "secret", "", "human-1")
	injectInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-escalate", "", "human-1", "init-3", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new inject init: %v", err)
	}
	if err := injectConn.WriteJSON(injectInit); err != nil {
		t.Fatalf("write inject init: %v", err)
	}
	awaitWSAck(t, injectConn, "init-3")
	awaitReadyEvent(t, injectConn)
	attachInject, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-escalate", "", "human-1", "attach-2", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "inject"})
	if err != nil {
		t.Fatalf("new attach inject: %v", err)
	}
	if err := injectConn.WriteJSON(attachInject); err != nil {
		t.Fatalf("write attach inject: %v", err)
	}
	ack := awaitWSAckEnvelope(t, injectConn, "attach-2")
	payload := decodeAckPayload(t, ack)
	attachment, _ := payload.Data["attachment"].(map[string]any)
	if attachment["client_id"] != "human-1" || attachment["mode"] != "inject" {
		t.Fatalf("unexpected attachment after escalation: %#v", payload.Data)
	}
	awaitStatusActionWS(t, orch, "mode_changed")

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-escalate", "", "human-1", "prompt-2", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "say hello"})
	if err != nil {
		t.Fatalf("new prompt after escalation: %v", err)
	}
	if err := injectConn.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt after escalation: %v", err)
	}
	awaitWSAck(t, injectConn, "prompt-2")
	awaitMessageComplete(t, orch, "hello world")
}

func TestRelayHumanInjectVisibleToOrchestrator(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-inject",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() { _ = wrapper.RunRelay(ctx, relayURL, "secret") }()
	waitForWrapperRegistration(t, srv, "sess-inject")

	orch := dialWS(t, relayURL)
	defer orch.Close()
	authenticateWSAs(t, orch, protocol.RoleClient, "secret", "", "orch-1")
	orchInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-inject", "", "orch-1", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new orch init: %v", err)
	}
	if err := orch.WriteJSON(orchInit); err != nil {
		t.Fatalf("write orch init: %v", err)
	}
	awaitWSAck(t, orch, "init-1")
	awaitReadyEvent(t, orch)

	human := dialWS(t, relayURL)
	defer human.Close()
	authenticateWSAs(t, human, protocol.RoleClient, "secret", "", "human-1")
	humanInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-inject", "", "human-1", "init-2", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new human init: %v", err)
	}
	if err := human.WriteJSON(humanInit); err != nil {
		t.Fatalf("write human init: %v", err)
	}
	awaitWSAck(t, human, "init-2")
	awaitReadyEvent(t, human)
	attachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-inject", "", "human-1", "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "inject"})
	if err != nil {
		t.Fatalf("new attach env: %v", err)
	}
	if err := human.WriteJSON(attachEnv); err != nil {
		t.Fatalf("write attach env: %v", err)
	}
	awaitWSAck(t, human, "attach-1")

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-inject", "", "human-1", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "say hello"})
	if err != nil {
		t.Fatalf("new prompt env: %v", err)
	}
	if err := human.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt env: %v", err)
	}
	awaitWSAck(t, human, "prompt-1")
	awaitMessageComplete(t, orch, "hello world")
}

func TestRelayTakeoverQueuesOrchestratorPromptsUntilDetach(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-takeover",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() { _ = wrapper.RunRelay(ctx, relayURL, "secret") }()
	waitForWrapperRegistration(t, srv, "sess-takeover")

	orch := dialWS(t, relayURL)
	defer orch.Close()
	authenticateWSAs(t, orch, protocol.RoleClient, "secret", "", "orch-1")
	orchInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-takeover", "", "orch-1", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new orch init: %v", err)
	}
	if err := orch.WriteJSON(orchInit); err != nil {
		t.Fatalf("write orch init: %v", err)
	}
	awaitWSAck(t, orch, "init-1")
	awaitReadyEvent(t, orch)

	human := dialWS(t, relayURL)
	defer human.Close()
	authenticateWSAs(t, human, protocol.RoleClient, "secret", "", "human-1")
	humanInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-takeover", "", "human-1", "init-2", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new human init: %v", err)
	}
	if err := human.WriteJSON(humanInit); err != nil {
		t.Fatalf("write human init: %v", err)
	}
	awaitWSAck(t, human, "init-2")
	awaitReadyEvent(t, human)
	attachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-takeover", "", "human-1", "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "takeover"})
	if err != nil {
		t.Fatalf("new attach env: %v", err)
	}
	if err := human.WriteJSON(attachEnv); err != nil {
		t.Fatalf("write attach env: %v", err)
	}
	attachAck := awaitWSAckEnvelope(t, human, "attach-1")
	attachPayload := decodeAckPayload(t, attachAck)
	attachment, _ := attachPayload.Data["attachment"].(map[string]any)
	if attachment["client_id"] != "human-1" || attachment["mode"] != "takeover" {
		t.Fatalf("unexpected takeover attachment payload: %#v", attachPayload.Data)
	}

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-takeover", "", "orch-1", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "say hello"})
	if err != nil {
		t.Fatalf("new prompt env: %v", err)
	}
	if err := orch.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt env: %v", err)
	}
	promptAck := awaitWSAckEnvelope(t, orch, "prompt-1")
	promptPayload := decodeAckPayload(t, promptAck)
	if queued, _ := promptPayload.Data["queued"].(bool); !queued {
		t.Fatalf("expected prompt to be queued during takeover: %#v", promptPayload.Data)
	}
	if queuedCount, _ := promptPayload.Data["queued_prompts"].(float64); int(queuedCount) != 1 {
		t.Fatalf("expected queued prompt count to be 1: %#v", promptPayload.Data)
	}

	statusEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-takeover", "", "orch-1", "status-1", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus})
	if err != nil {
		t.Fatalf("new status env: %v", err)
	}
	if err := orch.WriteJSON(statusEnv); err != nil {
		t.Fatalf("write status env: %v", err)
	}
	statusPayload := decodeAckPayload(t, awaitWSAckEnvelope(t, orch, "status-1"))
	if queuedCount, _ := statusPayload.Data["queued_prompts"].(float64); int(queuedCount) != 1 {
		t.Fatalf("expected status to report queued prompt: %#v", statusPayload.Data)
	}

	detachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-takeover", "", "human-1", "detach-1", protocol.SessionDetachCommand{Command: protocol.CommandSessionDetach})
	if err != nil {
		t.Fatalf("new detach env: %v", err)
	}
	if err := human.WriteJSON(detachEnv); err != nil {
		t.Fatalf("write detach env: %v", err)
	}
	awaitWSAck(t, human, "detach-1")
	awaitMessageComplete(t, orch, "hello world")

	statusEnv2, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-takeover", "", "orch-1", "status-2", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus})
	if err != nil {
		t.Fatalf("new second status env: %v", err)
	}
	if err := orch.WriteJSON(statusEnv2); err != nil {
		t.Fatalf("write second status env: %v", err)
	}
	statusPayload2 := decodeAckPayload(t, awaitWSAckEnvelope(t, orch, "status-2"))
	if queuedCount, _ := statusPayload2.Data["queued_prompts"].(float64); int(queuedCount) != 0 {
		t.Fatalf("expected queued prompts to flush after detach: %#v", statusPayload2.Data)
	}
}

func TestRelayPermissionFlow(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-perm",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "permission"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() {
		_ = wrapper.RunRelay(ctx, relayURL, "secret")
	}()
	waitForWrapperRegistration(t, srv, "sess-perm")

	clientConn := dialWS(t, relayURL)
	defer clientConn.Close()
	authenticateWS(t, clientConn, protocol.RoleClient, "secret", "")

	initEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm", "", "test-client", "init-1", protocol.InitializeCommand{
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

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm", "", "test-client", "prompt-1", protocol.SessionPromptCommand{
		Command: protocol.CommandSessionPrompt,
		Message: "need approval",
	})
	if err != nil {
		t.Fatalf("new prompt envelope: %v", err)
	}
	if err := clientConn.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	awaitWSAck(t, clientConn, "prompt-1")
	awaitPermissionRequestWS(t, clientConn, "perm-1")

	allowEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm", "", "test-client", "allow-1", protocol.PermissionResponseCommand{
		Command:   protocol.CommandSessionPermissionResponse,
		RequestID: "perm-1",
		Decision:  "allow",
	})
	if err != nil {
		t.Fatalf("new allow envelope: %v", err)
	}
	if err := clientConn.WriteJSON(allowEnv); err != nil {
		t.Fatalf("write allow: %v", err)
	}
	awaitWSAck(t, clientConn, "allow-1")
	awaitPermissionResolvedWS(t, clientConn, "perm-1", "allow")
	awaitMessageComplete(t, clientConn, "hello world")
}

func TestRelayPermissionAuthorityHeldByAttachedHuman(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-perm-authority",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "permission"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() { _ = wrapper.RunRelay(ctx, relayURL, "secret") }()
	waitForWrapperRegistration(t, srv, "sess-perm-authority")

	orch := dialWS(t, relayURL)
	defer orch.Close()
	authenticateWSAs(t, orch, protocol.RoleClient, "secret", "", "orch-1")
	orchInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-authority", "", "orch-1", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new orch init: %v", err)
	}
	if err := orch.WriteJSON(orchInit); err != nil {
		t.Fatalf("write orch init: %v", err)
	}
	awaitWSAck(t, orch, "init-1")
	awaitReadyEvent(t, orch)

	human := dialWS(t, relayURL)
	defer human.Close()
	authenticateWSAs(t, human, protocol.RoleClient, "secret", "", "human-1")
	humanInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-authority", "", "human-1", "init-2", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new human init: %v", err)
	}
	if err := human.WriteJSON(humanInit); err != nil {
		t.Fatalf("write human init: %v", err)
	}
	awaitWSAck(t, human, "init-2")
	awaitReadyEvent(t, human)
	attachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-authority", "", "human-1", "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "inject"})
	if err != nil {
		t.Fatalf("new attach env: %v", err)
	}
	if err := human.WriteJSON(attachEnv); err != nil {
		t.Fatalf("write attach env: %v", err)
	}
	awaitWSAck(t, human, "attach-1")

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-authority", "", "orch-1", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "need approval"})
	if err != nil {
		t.Fatalf("new prompt env: %v", err)
	}
	if err := orch.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt env: %v", err)
	}
	awaitWSAck(t, orch, "prompt-1")
	awaitPermissionRequestWS(t, orch, "perm-1")

	allowEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-authority", "", "orch-1", "allow-1", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: "perm-1", Decision: "allow"})
	if err != nil {
		t.Fatalf("new allow env: %v", err)
	}
	if err := orch.WriteJSON(allowEnv); err != nil {
		t.Fatalf("write allow env: %v", err)
	}
	awaitWSError(t, orch, "allow-1", "permission authority is held by attached human")

	humanAllow, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-authority", "", "human-1", "allow-2", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: "perm-1", Decision: "allow"})
	if err != nil {
		t.Fatalf("new human allow env: %v", err)
	}
	if err := human.WriteJSON(humanAllow); err != nil {
		t.Fatalf("write human allow env: %v", err)
	}
	awaitWSAck(t, human, "allow-2")
	awaitPermissionResolvedWS(t, orch, "perm-1", "allow")
}

func TestRelayTakeoverPTYInputVisibleToAttachedHuman(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:      "sess-pty",
		PTYBin:         helperProcessPath(t),
		PTYArgs:        []string{"-test.run=TestFakePTYProcess", "--"},
		Env:            map[string]string{"WEAVE_FAKE_PTY": "1"},
		StartupTimeout: 5 * time.Second,
	})
	go func() { _ = wrapper.RunRelay(ctx, relayURL, "secret") }()
	waitForWrapperRegistration(t, srv, "sess-pty")

	human := dialWS(t, relayURL)
	defer human.Close()
	authenticateWSAs(t, human, protocol.RoleClient, "secret", "", "human-1")
	initEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty", "", "human-1", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new init env: %v", err)
	}
	if err := human.WriteJSON(initEnv); err != nil {
		t.Fatalf("write init env: %v", err)
	}
	awaitWSAck(t, human, "init-1")
	awaitReadyEvent(t, human)
	attachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty", "", "human-1", "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "takeover"})
	if err != nil {
		t.Fatalf("new attach env: %v", err)
	}
	if err := human.WriteJSON(attachEnv); err != nil {
		t.Fatalf("write attach env: %v", err)
	}
	awaitWSAck(t, human, "attach-1")

	inputEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty", "", "human-1", "pty-1", protocol.PTYInputCommand{Command: protocol.CommandPTYInput, Data: "aGVsbG8NCg=="})
	if err != nil {
		t.Fatalf("new pty input env: %v", err)
	}
	if err := human.WriteJSON(inputEnv); err != nil {
		t.Fatalf("write pty input env: %v", err)
	}
	awaitWSAck(t, human, "pty-1")
	awaitPTYOutputContainsWS(t, human, "hello")

	resizeEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty", "", "human-1", "resize-1", protocol.PTYResizeCommand{Command: protocol.CommandPTYResize, Rows: 30, Cols: 90})
	if err != nil {
		t.Fatalf("new pty resize env: %v", err)
	}
	if err := human.WriteJSON(resizeEnv); err != nil {
		t.Fatalf("write pty resize env: %v", err)
	}
	awaitWSAck(t, human, "resize-1")

	statusEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty", "", "human-1", "status-1", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus})
	if err != nil {
		t.Fatalf("new status env: %v", err)
	}
	if err := human.WriteJSON(statusEnv); err != nil {
		t.Fatalf("write status env: %v", err)
	}
	statusPayload := decodeAckPayload(t, awaitWSAckEnvelope(t, human, "status-1"))
	if gotRows, _ := statusPayload.Data["pty_rows"].(float64); int(gotRows) != 30 {
		t.Fatalf("expected status to report pty_rows=30: %#v", statusPayload.Data)
	}
	if gotCols, _ := statusPayload.Data["pty_cols"].(float64); int(gotCols) != 90 {
		t.Fatalf("expected status to report pty_cols=90: %#v", statusPayload.Data)
	}
}

func TestRelayRejectsPTYInputWithoutTakeover(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:      "sess-pty-observe",
		PTYBin:         helperProcessPath(t),
		PTYArgs:        []string{"-test.run=TestFakePTYProcess", "--"},
		Env:            map[string]string{"WEAVE_FAKE_PTY": "1"},
		StartupTimeout: 5 * time.Second,
	})
	go func() { _ = wrapper.RunRelay(ctx, relayURL, "secret") }()
	waitForWrapperRegistration(t, srv, "sess-pty-observe")

	human := dialWS(t, relayURL)
	defer human.Close()
	authenticateWSAs(t, human, protocol.RoleClient, "secret", "", "human-1")
	initEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty-observe", "", "human-1", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new init env: %v", err)
	}
	if err := human.WriteJSON(initEnv); err != nil {
		t.Fatalf("write init env: %v", err)
	}
	awaitWSAck(t, human, "init-1")
	awaitReadyEvent(t, human)
	attachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty-observe", "", "human-1", "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "observe"})
	if err != nil {
		t.Fatalf("new attach env: %v", err)
	}
	if err := human.WriteJSON(attachEnv); err != nil {
		t.Fatalf("write attach env: %v", err)
	}
	awaitWSAck(t, human, "attach-1")

	inputEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty-observe", "", "human-1", "pty-1", protocol.PTYInputCommand{Command: protocol.CommandPTYInput, Data: "aGVsbG8NCg=="})
	if err != nil {
		t.Fatalf("new pty input env: %v", err)
	}
	if err := human.WriteJSON(inputEnv); err != nil {
		t.Fatalf("write pty input env: %v", err)
	}
	awaitWSError(t, human, "pty-1", "pty authority is held by attached human in takeover")
}

func TestRelayTakeoverAlsoHoldsPermissionAuthority(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-perm-takeover",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "permission"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() { _ = wrapper.RunRelay(ctx, relayURL, "secret") }()
	waitForWrapperRegistration(t, srv, "sess-perm-takeover")

	orch := dialWS(t, relayURL)
	defer orch.Close()
	authenticateWSAs(t, orch, protocol.RoleClient, "secret", "", "orch-1")
	orchInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-takeover", "", "orch-1", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new orch init: %v", err)
	}
	if err := orch.WriteJSON(orchInit); err != nil {
		t.Fatalf("write orch init: %v", err)
	}
	awaitWSAck(t, orch, "init-1")
	awaitReadyEvent(t, orch)

	human := dialWS(t, relayURL)
	defer human.Close()
	authenticateWSAs(t, human, protocol.RoleClient, "secret", "", "human-1")
	humanInit, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-takeover", "", "human-1", "init-2", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new human init: %v", err)
	}
	if err := human.WriteJSON(humanInit); err != nil {
		t.Fatalf("write human init: %v", err)
	}
	awaitWSAck(t, human, "init-2")
	awaitReadyEvent(t, human)
	attachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-takeover", "", "human-1", "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "takeover"})
	if err != nil {
		t.Fatalf("new attach env: %v", err)
	}
	if err := human.WriteJSON(attachEnv); err != nil {
		t.Fatalf("write attach env: %v", err)
	}
	awaitWSAck(t, human, "attach-1")

	humanPrompt, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-takeover", "", "human-1", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "need approval"})
	if err != nil {
		t.Fatalf("new human prompt env: %v", err)
	}
	if err := human.WriteJSON(humanPrompt); err != nil {
		t.Fatalf("write human prompt env: %v", err)
	}
	awaitWSAck(t, human, "prompt-1")
	awaitPermissionRequestWS(t, orch, "perm-1")

	allowEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-takeover", "", "orch-1", "allow-1", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: "perm-1", Decision: "allow"})
	if err != nil {
		t.Fatalf("new allow env: %v", err)
	}
	if err := orch.WriteJSON(allowEnv); err != nil {
		t.Fatalf("write allow env: %v", err)
	}
	awaitWSError(t, orch, "allow-1", "permission authority is held by attached human")

	humanAllow, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-takeover", "", "human-1", "allow-2", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: "perm-1", Decision: "allow"})
	if err != nil {
		t.Fatalf("new human allow env: %v", err)
	}
	if err := human.WriteJSON(humanAllow); err != nil {
		t.Fatalf("write human allow env: %v", err)
	}
	awaitWSAck(t, human, "allow-2")
	awaitPermissionResolvedWS(t, orch, "perm-1", "allow")
}

func TestRelayStatusShowsPendingPermission(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-perm-status",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "permission"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() {
		_ = wrapper.RunRelay(ctx, relayURL, "secret")
	}()
	waitForWrapperRegistration(t, srv, "sess-perm-status")

	clientConn := dialWS(t, relayURL)
	defer clientConn.Close()
	authenticateWS(t, clientConn, protocol.RoleClient, "secret", "")

	initEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-status", "", "test-client", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new init envelope: %v", err)
	}
	if err := clientConn.WriteJSON(initEnv); err != nil {
		t.Fatalf("write init: %v", err)
	}
	awaitWSAck(t, clientConn, "init-1")
	awaitReadyEvent(t, clientConn)

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-status", "", "test-client", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "need approval"})
	if err != nil {
		t.Fatalf("new prompt envelope: %v", err)
	}
	if err := clientConn.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	awaitWSAck(t, clientConn, "prompt-1")
	awaitPermissionRequestWS(t, clientConn, "perm-1")
	awaitStatusWS(t, clientConn, "waiting_permission")

	statusEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-status", "", "test-client", "status-1", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus})
	if err != nil {
		t.Fatalf("new status envelope: %v", err)
	}
	if err := clientConn.WriteJSON(statusEnv); err != nil {
		t.Fatalf("write status: %v", err)
	}
	ack := awaitWSAckEnvelope(t, clientConn, "status-1")
	payload := decodeAckPayload(t, ack)
	if payload.Data["state"] != "waiting_permission" || payload.Data["phase"] != "waiting_permission" {
		t.Fatalf("expected waiting permission status: %#v", payload.Data)
	}
	pending, _ := payload.Data["pending_permissions"].([]any)
	if len(pending) != 1 {
		t.Fatalf("expected one pending permission: %#v", payload.Data)
	}
	first, _ := pending[0].(map[string]any)
	if first["id"] != "perm-1" || first["kind"] != "confirm" {
		t.Fatalf("unexpected pending permission payload: %#v", first)
	}
}

func TestRelayInitializeDoesNotClearPendingPermission(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-perm-init",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "permission"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() {
		_ = wrapper.RunRelay(ctx, relayURL, "secret")
	}()
	waitForWrapperRegistration(t, srv, "sess-perm-init")

	clientConn := dialWS(t, relayURL)
	defer clientConn.Close()
	authenticateWS(t, clientConn, protocol.RoleClient, "secret", "")

	initEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-init", "", "test-client", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new init envelope: %v", err)
	}
	if err := clientConn.WriteJSON(initEnv); err != nil {
		t.Fatalf("write init: %v", err)
	}
	awaitWSAck(t, clientConn, "init-1")
	awaitReadyEvent(t, clientConn)

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-init", "", "test-client", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "need approval"})
	if err != nil {
		t.Fatalf("new prompt envelope: %v", err)
	}
	if err := clientConn.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	awaitWSAck(t, clientConn, "prompt-1")
	awaitPermissionRequestWS(t, clientConn, "perm-1")
	awaitStatusWS(t, clientConn, "waiting_permission")

	secondConn := dialWS(t, relayURL)
	defer secondConn.Close()
	authenticateWS(t, secondConn, protocol.RoleClient, "secret", "")
	secondInitEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-init", "", "test-client-2", "init-2", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new second init envelope: %v", err)
	}
	if err := secondConn.WriteJSON(secondInitEnv); err != nil {
		t.Fatalf("write second init: %v", err)
	}
	awaitWSAck(t, secondConn, "init-2")
	awaitReadyEvent(t, secondConn)

	statusEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-init", "", "test-client", "status-1", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus})
	if err != nil {
		t.Fatalf("new status envelope: %v", err)
	}
	if err := clientConn.WriteJSON(statusEnv); err != nil {
		t.Fatalf("write status: %v", err)
	}
	ack := awaitWSAckEnvelope(t, clientConn, "status-1")
	payload := decodeAckPayload(t, ack)
	pending, _ := payload.Data["pending_permissions"].([]any)
	if len(pending) != 1 {
		t.Fatalf("expected pending permission to survive reinitialize: %#v", payload.Data)
	}
}

func TestRelayRejectsDuplicatePermissionResponse(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-perm-dup",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "permission"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() {
		_ = wrapper.RunRelay(ctx, relayURL, "secret")
	}()
	waitForWrapperRegistration(t, srv, "sess-perm-dup")

	clientConn := dialWS(t, relayURL)
	defer clientConn.Close()
	authenticateWS(t, clientConn, protocol.RoleClient, "secret", "")

	initEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-dup", "", "test-client", "init-1", protocol.InitializeCommand{Command: protocol.CommandInitialize, ProtocolVersion: protocol.Version})
	if err != nil {
		t.Fatalf("new init envelope: %v", err)
	}
	if err := clientConn.WriteJSON(initEnv); err != nil {
		t.Fatalf("write init: %v", err)
	}
	awaitWSAck(t, clientConn, "init-1")
	awaitReadyEvent(t, clientConn)

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-dup", "", "test-client", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "need approval"})
	if err != nil {
		t.Fatalf("new prompt envelope: %v", err)
	}
	if err := clientConn.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	awaitWSAck(t, clientConn, "prompt-1")
	awaitPermissionRequestWS(t, clientConn, "perm-1")

	allowEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-dup", "", "test-client", "allow-1", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: "perm-1", Decision: "allow"})
	if err != nil {
		t.Fatalf("new allow envelope: %v", err)
	}
	if err := clientConn.WriteJSON(allowEnv); err != nil {
		t.Fatalf("write allow: %v", err)
	}
	awaitWSAck(t, clientConn, "allow-1")
	awaitPermissionResolvedWS(t, clientConn, "perm-1", "allow")

	dupeEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-perm-dup", "", "test-client", "allow-2", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: "perm-1", Decision: "allow"})
	if err != nil {
		t.Fatalf("new duplicate allow envelope: %v", err)
	}
	if err := clientConn.WriteJSON(dupeEnv); err != nil {
		t.Fatalf("write duplicate allow: %v", err)
	}
	awaitWSError(t, clientConn, "allow-2", `unknown permission request "perm-1"`)
}

func TestRelayAttachMovesOwnershipBetweenSessions(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, sessionID := range []string{"sess-attach-a", "sess-attach-b"} {
		wrapper := local.NewWrapper(local.Config{
			SessionID:       sessionID,
			PiBin:           helperProcessPath(t),
			PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
			Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
			NoDefaultPiArgs: true,
			StartupTimeout:  5 * time.Second,
		})
		go func(wrapper *local.Wrapper) {
			_ = wrapper.RunRelay(ctx, relayURL, "secret")
		}(wrapper)
		waitForWrapperRegistration(t, srv, sessionID)
	}

	clientConn := dialWS(t, relayURL)
	defer clientConn.Close()
	authenticateWSAs(t, clientConn, protocol.RoleClient, "secret", "", "controller")

	attachA, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-attach-a", "", "controller", "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "observe"})
	if err != nil {
		t.Fatalf("new attach-a envelope: %v", err)
	}
	if err := clientConn.WriteJSON(attachA); err != nil {
		t.Fatalf("write attach-a: %v", err)
	}
	awaitWSAck(t, clientConn, "attach-1")

	statusA, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-attach-a", "", "controller", "status-1", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus})
	if err != nil {
		t.Fatalf("new status-a envelope: %v", err)
	}
	if err := clientConn.WriteJSON(statusA); err != nil {
		t.Fatalf("write status-a: %v", err)
	}
	statusAPayload := decodeAckPayload(t, awaitWSAckEnvelope(t, clientConn, "status-1"))
	attachmentA, _ := statusAPayload.Data["attachment"].(map[string]any)
	if stringValue(attachmentA["client_id"]) != "controller" || stringValue(attachmentA["mode"]) != "observe" {
		t.Fatalf("expected observe attachment on sess-attach-a: %#v", statusAPayload.Data)
	}

	attachB, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-attach-b", "", "controller", "attach-2", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "inject"})
	if err != nil {
		t.Fatalf("new attach-b envelope: %v", err)
	}
	if err := clientConn.WriteJSON(attachB); err != nil {
		t.Fatalf("write attach-b: %v", err)
	}
	awaitWSAck(t, clientConn, "attach-2")

	statusA2, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-attach-a", "", "controller", "status-2", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus})
	if err != nil {
		t.Fatalf("new status-a2 envelope: %v", err)
	}
	if err := clientConn.WriteJSON(statusA2); err != nil {
		t.Fatalf("write status-a2: %v", err)
	}
	statusA2Payload := decodeAckPayload(t, awaitWSAckEnvelope(t, clientConn, "status-2"))
	if attachment, ok := statusA2Payload.Data["attachment"]; ok && attachment != nil {
		t.Fatalf("expected sess-attach-a attachment to be cleared: %#v", statusA2Payload.Data)
	}

	statusB, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-attach-b", "", "controller", "status-3", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus})
	if err != nil {
		t.Fatalf("new status-b envelope: %v", err)
	}
	if err := clientConn.WriteJSON(statusB); err != nil {
		t.Fatalf("write status-b: %v", err)
	}
	statusBPayload := decodeAckPayload(t, awaitWSAckEnvelope(t, clientConn, "status-3"))
	attachmentB, _ := statusBPayload.Data["attachment"].(map[string]any)
	if stringValue(attachmentB["client_id"]) != "controller" || stringValue(attachmentB["mode"]) != "inject" {
		t.Fatalf("expected inject attachment on sess-attach-b: %#v", statusBPayload.Data)
	}
}

func TestRelayObserveAttachmentBlocksPromptFromOwner(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-observe",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() {
		_ = wrapper.RunRelay(ctx, relayURL, "secret")
	}()
	waitForWrapperRegistration(t, srv, "sess-observe")

	clientConn := dialWS(t, relayURL)
	defer clientConn.Close()
	authenticateWSAs(t, clientConn, protocol.RoleClient, "secret", "", "controller")

	attachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-observe", "", "controller", "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "observe"})
	if err != nil {
		t.Fatalf("new attach envelope: %v", err)
	}
	if err := clientConn.WriteJSON(attachEnv); err != nil {
		t.Fatalf("write attach: %v", err)
	}
	awaitWSAck(t, clientConn, "attach-1")

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-observe", "", "controller", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: "say hello"})
	if err != nil {
		t.Fatalf("new prompt envelope: %v", err)
	}
	if err := clientConn.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	awaitWSError(t, clientConn, "prompt-1", "attached client in observe mode cannot send prompts")
}

func TestRelayListSessions(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-list",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() {
		_ = wrapper.RunRelay(ctx, relayURL, "secret")
	}()
	waitForWrapperRegistration(t, srv, "sess-list")

	clientConn := dialWS(t, relayURL)
	defer clientConn.Close()
	authenticateWS(t, clientConn, protocol.RoleClient, "secret", "")

	listEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "", "", "test-client", "sessions-1", protocol.ListSessionsCommand{
		Command: protocol.CommandRegistryListSessions,
	})
	if err != nil {
		t.Fatalf("new list envelope: %v", err)
	}
	if err := clientConn.WriteJSON(listEnv); err != nil {
		t.Fatalf("write list: %v", err)
	}
	ack := awaitWSAckEnvelope(t, clientConn, "sessions-1")
	payload := decodeAckPayload(t, ack)
	items, _ := payload.Data["sessions"].([]any)
	if len(items) == 0 {
		t.Fatalf("expected at least one session in list: %#v", payload.Data)
	}
	first, _ := items[0].(map[string]any)
	if nestedString(first, "session", "id") != "sess-list" {
		t.Fatalf("unexpected session list payload: %#v", payload.Data)
	}
}

func TestRelaySpawnLoadAndRuntimeStop(t *testing.T) {
	tempDir := t.TempDir()
	launcher := newFakeLauncher(t)
	srv := NewServer(Config{Token: "secret", SessionDir: tempDir, StartupTimeout: 5 * time.Second, Launcher: launcher})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"
	srv.cfg.PublicURL = relayURL

	clientConn := dialWS(t, relayURL)
	defer clientConn.Close()
	authenticateWS(t, clientConn, protocol.RoleClient, "secret", "")

	spawnEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-spawn", "", "test-client", "spawn-1", protocol.SessionSpawnCommand{
		Command: protocol.CommandSessionSpawn,
	})
	if err != nil {
		t.Fatalf("new spawn envelope: %v", err)
	}
	if err := clientConn.WriteJSON(spawnEnv); err != nil {
		t.Fatalf("write spawn: %v", err)
	}
	spawnAck := awaitWSAckEnvelope(t, clientConn, "spawn-1")
	spawnPayload := decodeAckPayload(t, spawnAck)
	firstRuntimeID := nestedString(spawnPayload.Data, "runtime", "id")
	persistedHandle := stringValue(spawnPayload.Data["persisted_session_handle"])
	if firstRuntimeID == "" || persistedHandle == "" {
		t.Fatalf("expected runtime id and persisted handle: %#v", spawnPayload.Data)
	}

	initEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-spawn", "", "test-client", "init-spawn", protocol.InitializeCommand{
		Command:         protocol.CommandInitialize,
		ProtocolVersion: protocol.Version,
	})
	if err != nil {
		t.Fatalf("new init envelope: %v", err)
	}
	if err := clientConn.WriteJSON(initEnv); err != nil {
		t.Fatalf("write init after spawn: %v", err)
	}
	awaitWSAck(t, clientConn, "init-spawn")
	awaitReadyEvent(t, clientConn)

	stopEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-spawn", "", "test-client", "stop-1", protocol.RuntimeStopCommand{
		Command: protocol.CommandRuntimeStop,
	})
	if err != nil {
		t.Fatalf("new stop envelope: %v", err)
	}
	if err := clientConn.WriteJSON(stopEnv); err != nil {
		t.Fatalf("write stop: %v", err)
	}
	stopAck := awaitWSAckEnvelope(t, clientConn, "stop-1")
	stopPayload := decodeAckPayload(t, stopAck)
	if connected, _ := stopPayload.Data["wrapper_connected"].(bool); connected {
		t.Fatalf("expected wrapper to be disconnected: %#v", stopPayload.Data)
	}

	loadEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-spawn", "", "test-client", "load-1", protocol.SessionLoadCommand{
		Command: protocol.CommandSessionLoad,
	})
	if err != nil {
		t.Fatalf("new load envelope: %v", err)
	}
	if err := clientConn.WriteJSON(loadEnv); err != nil {
		t.Fatalf("write load: %v", err)
	}
	loadAck := awaitWSAckEnvelope(t, clientConn, "load-1")
	loadPayload := decodeAckPayload(t, loadAck)
	secondRuntimeID := nestedString(loadPayload.Data, "runtime", "id")
	if secondRuntimeID == "" || secondRuntimeID == firstRuntimeID {
		t.Fatalf("expected a new runtime id after load: first=%s second=%s payload=%#v", firstRuntimeID, secondRuntimeID, loadPayload.Data)
	}
	if stringValue(loadPayload.Data["persisted_session_handle"]) != persistedHandle {
		t.Fatalf("expected persisted handle to survive load: %#v", loadPayload.Data)
	}

	initLoadEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-spawn", "", "test-client", "init-load", protocol.InitializeCommand{
		Command:         protocol.CommandInitialize,
		ProtocolVersion: protocol.Version,
	})
	if err != nil {
		t.Fatalf("new init-load envelope: %v", err)
	}
	if err := clientConn.WriteJSON(initLoadEnv); err != nil {
		t.Fatalf("write init after load: %v", err)
	}
	awaitWSAck(t, clientConn, "init-load")
	awaitReadyEvent(t, clientConn)

	promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-spawn", "", "test-client", "prompt-1", protocol.SessionPromptCommand{
		Command: protocol.CommandSessionPrompt,
		Message: "say hello",
	})
	if err != nil {
		t.Fatalf("new prompt envelope: %v", err)
	}
	if err := clientConn.WriteJSON(promptEnv); err != nil {
		t.Fatalf("write prompt after load: %v", err)
	}
	awaitWSAck(t, clientConn, "prompt-1")
	awaitMessageComplete(t, clientConn, "hello world")
}

func TestRelayRejectsSpawnAndLoadPathTraversal(t *testing.T) {
	tempDir := t.TempDir()
	srv := NewServer(Config{Token: "secret", SessionDir: tempDir})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	conn := dialWS(t, relayURL)
	defer conn.Close()
	authenticateWS(t, conn, protocol.RoleClient, "secret", "")

	spawnEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "../escape", "", "test-client", "spawn-1", protocol.SessionSpawnCommand{
		Command: protocol.CommandSessionSpawn,
	})
	if err != nil {
		t.Fatalf("new spawn envelope: %v", err)
	}
	if err := conn.WriteJSON(spawnEnv); err != nil {
		t.Fatalf("write spawn: %v", err)
	}
	awaitWSError(t, conn, "spawn-1", `invalid session_id "../escape"`)

	loadEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-load", "", "test-client", "load-1", protocol.SessionLoadCommand{
		Command:     protocol.CommandSessionLoad,
		SessionPath: "../escape.jsonl",
	})
	if err != nil {
		t.Fatalf("new load envelope: %v", err)
	}
	if err := conn.WriteJSON(loadEnv); err != nil {
		t.Fatalf("write load: %v", err)
	}
	awaitWSError(t, conn, "load-1", "session_path must stay within "+tempDir)
}

func TestRelaySpawnAndLoadPTYTransport(t *testing.T) {
	tempDir := t.TempDir()
	launcher := newFakeLauncher(t)
	srv := NewServer(Config{Token: "secret", SessionDir: tempDir, PublicURL: "ws://127.0.0.1:0/ws", Launcher: launcher})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"
	srv.cfg.PublicURL = relayURL

	conn := dialWS(t, relayURL)
	defer conn.Close()
	authenticateWS(t, conn, protocol.RoleClient, "secret", "")

	spawnEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty-spawn", "", "test-client", "spawn-1", protocol.SessionSpawnCommand{
		Command:   protocol.CommandSessionSpawn,
		Transport: "pty",
	})
	if err != nil {
		t.Fatalf("new pty spawn envelope: %v", err)
	}
	if err := conn.WriteJSON(spawnEnv); err != nil {
		t.Fatalf("write pty spawn: %v", err)
	}
	spawnPayload := decodeAckPayload(t, awaitWSAckEnvelope(t, conn, "spawn-1"))
	if nestedString(spawnPayload.Data, "runtime", "transport") != "pty" {
		t.Fatalf("expected pty runtime transport after spawn: %#v", spawnPayload.Data)
	}

	stopEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty-spawn", "", "test-client", "stop-1", protocol.RuntimeStopCommand{Command: protocol.CommandRuntimeStop})
	if err != nil {
		t.Fatalf("new stop envelope: %v", err)
	}
	if err := conn.WriteJSON(stopEnv); err != nil {
		t.Fatalf("write stop: %v", err)
	}
	awaitWSAck(t, conn, "stop-1")

	loadEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-pty-spawn", "", "test-client", "load-1", protocol.SessionLoadCommand{
		Command:   protocol.CommandSessionLoad,
		Transport: "pty",
	})
	if err != nil {
		t.Fatalf("new pty load envelope: %v", err)
	}
	if err := conn.WriteJSON(loadEnv); err != nil {
		t.Fatalf("write pty load: %v", err)
	}
	loadPayload := decodeAckPayload(t, awaitWSAckEnvelope(t, conn, "load-1"))
	if nestedString(loadPayload.Data, "runtime", "transport") != "pty" {
		t.Fatalf("expected pty runtime transport after load: %#v", loadPayload.Data)
	}
}

func TestRelayDuplicateWrapperAuthDoesNotAuthenticateConnection(t *testing.T) {
	srv := NewServer(Config{Token: "secret"})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	relayURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SessionID:       "sess-auth",
		PiBin:           helperProcessPath(t),
		PiArgs:          []string{"-test.run=TestFakePIProcess", "--"},
		Env:             map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"},
		NoDefaultPiArgs: true,
		StartupTimeout:  5 * time.Second,
	})
	go func() { _ = wrapper.RunRelay(ctx, relayURL, "secret") }()
	waitForWrapperRegistration(t, srv, "sess-auth")

	dup := dialWS(t, relayURL)
	defer dup.Close()
	auth, err := protocol.NewEnvelope(protocol.MessageCommand, "sess-auth", "rt-dup", "dup-wrapper", "auth-1", protocol.AuthCommand{
		Command: protocol.CommandAuth,
		Token:   "secret",
		Role:    protocol.RoleWrapper,
	})
	if err != nil {
		t.Fatalf("new auth envelope: %v", err)
	}
	if err := dup.WriteJSON(auth); err != nil {
		t.Fatalf("write duplicate auth: %v", err)
	}
	awaitWSError(t, dup, "auth-1", "wrapper already registered for session")

	event, err := protocol.NewEnvelope(protocol.MessageEvent, "sess-auth", "rt-dup", "dup-wrapper", "evt-1", protocol.SessionUpdateEvent{
		Event:  protocol.EventSessionUpdate,
		Update: protocol.SessionUpdate{Kind: protocol.UpdateStatus, Phase: "running"},
	})
	if err != nil {
		t.Fatalf("new spoofed event: %v", err)
	}
	if err := dup.WriteJSON(event); err != nil {
		t.Fatalf("write spoofed event: %v", err)
	}
	awaitWSError(t, dup, "evt-1", "auth required before other message types")
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

type fakeLaunchState struct {
	cancel    context.CancelFunc
	transport string
}

type fakeLauncher struct {
	t      *testing.T
	mu     sync.Mutex
	states map[string]*fakeLaunchState
}

func newFakeLauncher(t *testing.T) *fakeLauncher {
	return &fakeLauncher{t: t, states: make(map[string]*fakeLaunchState)}
}

func (l *fakeLauncher) Spawn(_ context.Context, req LaunchRequest) error {
	l.mu.Lock()
	if l.states[req.SessionID] != nil {
		l.mu.Unlock()
		return errRuntimeAlreadyManaged
	}
	ctx, cancel := context.WithCancel(context.Background())
	state := &fakeLaunchState{cancel: cancel, transport: req.RuntimeDescriptor.Transport}
	l.states[req.SessionID] = state
	l.mu.Unlock()

	if req.PersistedSessionHandle != "" {
		if err := os.MkdirAll(filepath.Dir(req.PersistedSessionHandle), 0o755); err != nil {
			return err
		}
		if _, err := os.Stat(req.PersistedSessionHandle); err != nil {
			if err := os.WriteFile(req.PersistedSessionHandle, nil, 0o644); err != nil {
				return err
			}
		}
	}

	config := local.Config{
		SessionID:      req.SessionID,
		StartupTimeout: 5 * time.Second,
	}
	if req.RuntimeDescriptor.Transport == "pty" {
		config.PTYBin = helperProcessPath(l.t)
		config.PTYArgs = []string{"-test.run=TestFakePTYProcess", "--"}
		config.Env = map[string]string{"WEAVE_FAKE_PTY": "1"}
	} else {
		config.PiBin = helperProcessPath(l.t)
		config.PiArgs = []string{"-test.run=TestFakePIProcess", "--", "--session=" + req.PersistedSessionHandle}
		config.Env = map[string]string{"WEAVE_FAKE_PI": "1", "WEAVE_FAKE_PI_SCENARIO": "stream"}
		config.NoDefaultPiArgs = true
	}
	wrapper := local.NewWrapper(config)
	go func() {
		_ = wrapper.RunRelay(ctx, req.RelayURL, req.Token)
		l.mu.Lock()
		if l.states[req.SessionID] == state {
			delete(l.states, req.SessionID)
		}
		l.mu.Unlock()
	}()
	return nil
}

func (l *fakeLauncher) Stop(sessionID string) error {
	l.mu.Lock()
	state := l.states[sessionID]
	if state != nil {
		delete(l.states, sessionID)
	}
	l.mu.Unlock()
	if state == nil {
		return os.ErrNotExist
	}
	state.cancel()
	return nil
}

func (l *fakeLauncher) Shutdown(_ context.Context) error {
	l.mu.Lock()
	states := make([]*fakeLaunchState, 0, len(l.states))
	for sessionID, state := range l.states {
		delete(l.states, sessionID)
		states = append(states, state)
	}
	l.mu.Unlock()
	for _, state := range states {
		state.cancel()
	}
	return nil
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

func TestFakePTYProcess(t *testing.T) {
	if os.Getenv("WEAVE_FAKE_PTY") != "1" {
		return
	}
	_, _ = io.Copy(os.Stdout, os.Stdin)
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
	permissionResolved := make(chan bool, 1)
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
				if scenario == "permission" {
					time.Sleep(20 * time.Millisecond)
					_ = protocol.WriteJSONLine(os.Stdout, map[string]any{"type": "extension_ui_request", "id": "perm-1", "method": "confirm", "title": "Allow file write?", "message": "Need approval"})
					allowed := <-permissionResolved
					if !allowed {
						_ = protocol.WriteJSONLine(os.Stdout, map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "error", "reason": "permission denied"}, "message": map[string]any{"role": "assistant"}})
						_ = protocol.WriteJSONLine(os.Stdout, map[string]any{"type": "agent_end", "messages": []any{}})
						return
					}
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
		case "extension_ui_response":
			if cmd["id"] == "perm-1" {
				confirmed, _ := cmd["confirmed"].(bool)
				permissionResolved <- confirmed
			}
			return nil
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
	authenticateWSAs(t, conn, role, token, sessionID, "test")
}

func authenticateWSAs(t *testing.T, conn *websocket.Conn, role, token, sessionID, identity string) {
	t.Helper()
	env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", identity, "auth-1", protocol.AuthCommand{
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
	_ = awaitWSAckEnvelope(t, conn, id)
}

func awaitWSAckEnvelope(t *testing.T, conn *websocket.Conn, id string) protocol.Envelope {
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
			return env
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

func awaitWSError(t *testing.T, conn *websocket.Conn, id, want string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("read error envelope: %v", err)
		}
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
	}
}

func decodeAckPayload(t *testing.T, env protocol.Envelope) protocol.AckPayload {
	t.Helper()
	var payload protocol.AckPayload
	if err := env.DecodePayload(&payload); err != nil {
		t.Fatalf("decode ack payload: %v", err)
	}
	return payload
}

func nestedString(data map[string]any, key, nested string) string {
	inner, _ := data[key].(map[string]any)
	return stringValue(inner[nested])
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func awaitPermissionRequestWS(t *testing.T, conn *websocket.Conn, requestID string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("read permission request: %v", err)
		}
		if env.Type != protocol.MessageEvent {
			continue
		}
		var evt protocol.SessionUpdateEvent
		if err := env.DecodePayload(&evt); err != nil || evt.Event != protocol.EventSessionUpdate {
			continue
		}
		if evt.Update.Kind == protocol.UpdatePermissionRequest && evt.Update.RequestID == requestID {
			return
		}
	}
}

func awaitPTYOutputContainsWS(t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("read pty output: %v", err)
		}
		if env.Type != protocol.MessageEvent {
			continue
		}
		var evt protocol.PTYOutputEvent
		if err := env.DecodePayload(&evt); err != nil || evt.Event != protocol.EventPTYOutput {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(evt.Data)
		if err != nil {
			t.Fatalf("decode pty output: %v", err)
		}
		if strings.Contains(string(data), want) {
			return
		}
	}
}

func awaitStatusActionWS(t *testing.T, conn *websocket.Conn, action string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("read status action event: %v", err)
		}
		if env.Type != protocol.MessageEvent {
			continue
		}
		var evt protocol.SessionUpdateEvent
		if err := env.DecodePayload(&evt); err != nil || evt.Event != protocol.EventSessionUpdate {
			continue
		}
		if evt.Update.Kind == protocol.UpdateStatus && stringValue(evt.Update.Details["attachment_action"]) == action {
			return
		}
	}
}

func awaitStatusWS(t *testing.T, conn *websocket.Conn, phase string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("read status event: %v", err)
		}
		if env.Type != protocol.MessageEvent {
			continue
		}
		var evt protocol.SessionUpdateEvent
		if err := env.DecodePayload(&evt); err != nil || evt.Event != protocol.EventSessionUpdate {
			continue
		}
		if evt.Update.Kind == protocol.UpdateStatus && evt.Update.Phase == phase {
			return
		}
	}
}

func awaitPermissionResolvedWS(t *testing.T, conn *websocket.Conn, requestID, decision string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("read permission resolved: %v", err)
		}
		if env.Type != protocol.MessageEvent {
			continue
		}
		var evt protocol.SessionUpdateEvent
		if err := env.DecodePayload(&evt); err != nil || evt.Event != protocol.EventSessionUpdate {
			continue
		}
		if evt.Update.Kind == protocol.UpdatePermissionResolved && evt.Update.RequestID == requestID && evt.Update.Decision == decision {
			return
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
