package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gorilla/websocket"
	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
	"golang.org/x/sys/unix"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "weave-inspect:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: weave-inspect <local|relay> [flags] <tui|init|sessions|status|spawn|load|switch-transport|kill-runtime|watch|attach|takeover|detach|release|inject|pty-input|pty-resize|prompt|cancel|allow|deny> [message]")
	}
	switch os.Args[1] {
	case "local":
		return runLocal(os.Args[2:])
	case "relay":
		return runRelay(os.Args[2:])
	default:
		return fmt.Errorf("usage: weave-inspect <local|relay> [flags] <tui|init|sessions|status|spawn|load|switch-transport|kill-runtime|watch|attach|takeover|detach|release|inject|pty-input|pty-resize|prompt|cancel|allow|deny> [message]")
	}
}

func runLocal(args []string) error {
	fs := flag.NewFlagSet("weave-inspect local", flag.ContinueOnError)
	socket := fs.String("socket", "/tmp/weave-local.sock", "Unix socket path")
	sessionID := fs.String("session", "local", "Logical session id")
	jsonMode := fs.Bool("json", false, "Print raw JSON envelopes")
	delivery := fs.String("delivery", "", "Prompt delivery mode: default, foreground, interrupt, queue, deliver_when_idle")
	if err := fs.Parse(args); err != nil {
		return err
	}
	subcmd, message, err := parseSubcommand(fs.Args())
	if err != nil {
		return err
	}
	if subcmd == "tui" || subcmd == "sessions" || subcmd == "spawn" || subcmd == "load" || subcmd == "switch-transport" || subcmd == "kill-runtime" || subcmd == "watch" || subcmd == "attach" || subcmd == "takeover" || subcmd == "detach" || subcmd == "release" || subcmd == "inject" || subcmd == "pty-input" || subcmd == "pty-resize" {
		return fmt.Errorf("%s is only supported in relay mode", subcmd)
	}

	conn, err := net.Dial("unix", *socket)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := newLocalClient(conn)
	if err := client.initialize(*sessionID, *jsonMode); err != nil {
		return err
	}
	return client.execute(subcmd, message, *delivery, "", *sessionID, *jsonMode)
}

func runRelay(args []string) error {
	fs := flag.NewFlagSet("weave-inspect relay", flag.ContinueOnError)
	relayURL := fs.String("relay", "", "Relay websocket URL")
	token := fs.String("token", "", "Shared bearer token")
	sessionID := fs.String("session", "", "Logical session id")
	jsonMode := fs.Bool("json", false, "Print raw JSON envelopes")
	delivery := fs.String("delivery", "", "Prompt delivery mode: default, foreground, interrupt, queue, deliver_when_idle")
	transport := fs.String("transport", "", "Spawn/load transport: rpc or pty")
	identity := fs.String("identity", "", "Client identity used for attach/inject/takeover authority")
	debugKeysFile := fs.String("debug-keys-file", "", "Append takeover stdin bytes to this file for debugging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	subcmd, message, err := parseSubcommand(fs.Args())
	if err != nil {
		return err
	}

	saved, _ := loadRelayContext()
	resolved := resolveRelayContext(saved, relayContext{
		RelayURL:      *relayURL,
		Token:         *token,
		SessionID:     *sessionID,
		Identity:      *identity,
		DebugKeysFile: *debugKeysFile,
	})
	if subcmd == "tui" {
		return runRelayTUI(resolved)
	}

	conn, _, err := websocket.DefaultDialer.Dial(resolved.RelayURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := newRelayClient(conn, resolved.Token, resolved.Identity, resolved.DebugKeysFile)
	if err := client.authenticate(*jsonMode); err != nil {
		return err
	}
	if shouldInitializeRelay(subcmd) {
		if err := client.initialize(resolved.SessionID, *jsonMode); err != nil {
			return err
		}
	}
	if err := client.execute(subcmd, message, *delivery, *transport, resolved.SessionID, *jsonMode); err != nil {
		return err
	}
	_ = saveRelayContext(resolved)
	return nil
}

type relayContext struct {
	RelayURL      string `json:"relay_url,omitempty"`
	Token         string `json:"token,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	Identity      string `json:"identity,omitempty"`
	DebugKeysFile string `json:"debug_keys_file,omitempty"`
}

func relayContextPath() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(cwd))
	return filepath.Join(cacheDir, "agentic-weave", "weave-inspect", fmt.Sprintf("%x.json", sum[:8])), nil
}

func loadRelayContext() (relayContext, error) {
	path, err := relayContextPath()
	if err != nil {
		return relayContext{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return relayContext{}, err
	}
	var ctx relayContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return relayContext{}, err
	}
	return ctx, nil
}

func saveRelayContext(ctx relayContext) error {
	path, err := relayContextPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveRelayContext(saved, override relayContext) relayContext {
	return relayContext{
		RelayURL:      firstNonEmpty(override.RelayURL, os.Getenv("WEAVE_RELAY_URL"), saved.RelayURL, "ws://localhost:8080/ws"),
		Token:         firstNonEmpty(override.Token, os.Getenv("WEAVE_TOKEN"), saved.Token),
		SessionID:     firstNonEmpty(override.SessionID, os.Getenv("WEAVE_SESSION"), saved.SessionID, "local"),
		Identity:      firstNonEmpty(override.Identity, os.Getenv("WEAVE_IDENTITY"), saved.Identity, "weave-inspect"),
		DebugKeysFile: firstNonEmpty(override.DebugKeysFile, os.Getenv("WEAVE_DEBUG_KEYS_FILE"), saved.DebugKeysFile),
	}
}

type sessionSummary struct {
	SessionID          string
	RuntimeID          string
	RuntimeKind        string
	RuntimeTransport   string
	PersistedHandle    string
	State              string
	Phase              string
	WrapperConnected   bool
	AttachedClientID   string
	AttachedMode       string
	AttachedReturnMode string
	QueuedPrompts      int
	PendingPermissions int
	PTYRows            int
	PTYCols            int
	UpdatedAt          string
}

func parseSessionSummaryData(data map[string]any) sessionSummary {
	sessionMap, _ := data["session"].(map[string]any)
	runtimeMap, _ := data["runtime"].(map[string]any)
	attachmentMap, _ := data["attachment"].(map[string]any)
	pendingPermissions := 0
	if pending, ok := data["pending_permissions"].([]any); ok {
		pendingPermissions = len(pending)
	}
	return sessionSummary{
		SessionID:          stringValue(sessionMap["id"]),
		RuntimeID:          stringValue(runtimeMap["id"]),
		RuntimeKind:        stringValue(runtimeMap["kind"]),
		RuntimeTransport:   stringValue(runtimeMap["transport"]),
		PersistedHandle:    stringValue(data["persisted_session_handle"]),
		State:              stringValue(data["state"]),
		Phase:              stringValue(data["phase"]),
		WrapperConnected:   boolValue(data["wrapper_connected"]),
		AttachedClientID:   stringValue(attachmentMap["client_id"]),
		AttachedMode:       stringValue(attachmentMap["mode"]),
		AttachedReturnMode: stringValue(attachmentMap["return_transport"]),
		QueuedPrompts:      intValueLoose(data["queued_prompts"]),
		PendingPermissions: pendingPermissions,
		PTYRows:            intValueLoose(data["pty_rows"]),
		PTYCols:            intValueLoose(data["pty_cols"]),
		UpdatedAt:          stringValue(data["updated_at"]),
	}
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func intValueLoose(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func parseSubcommand(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("missing subcommand: tui, init, sessions, status, spawn, load, switch-transport, kill-runtime, watch, attach, takeover, detach, release, inject, pty-input, pty-resize, prompt, cancel, allow, or deny")
	}
	subcmd := args[0]
	switch subcmd {
	case "tui", "init", "sessions", "status", "spawn", "load", "kill-runtime", "watch", "takeover", "detach", "release", "cancel":
		return subcmd, "", nil
	case "switch-transport":
		if len(args) < 2 {
			return "", "", fmt.Errorf("switch-transport requires a transport: rpc or pty")
		}
		return subcmd, args[1], nil
	case "attach":
		if len(args) < 2 {
			return "", "", fmt.Errorf("attach requires a mode: observe, inject, or takeover")
		}
		return subcmd, args[1], nil
	case "inject":
		if len(args) < 2 {
			return "", "", fmt.Errorf("inject requires a message")
		}
		return subcmd, strings.Join(args[1:], " "), nil
	case "pty-input":
		if len(args) < 2 {
			return "", "", fmt.Errorf("pty-input requires text to send")
		}
		return subcmd, strings.Join(args[1:], " "), nil
	case "pty-resize":
		if len(args) != 3 {
			return "", "", fmt.Errorf("pty-resize requires rows and cols")
		}
		return subcmd, strings.Join(args[1:], " "), nil
	case "allow", "deny":
		if len(args) < 2 {
			return "", "", fmt.Errorf("%s requires a request id", subcmd)
		}
		return subcmd, args[1], nil
	case "prompt":
		if len(args) < 2 {
			return "", "", fmt.Errorf("prompt requires a message")
		}
		return subcmd, strings.Join(args[1:], " "), nil
	default:
		return "", "", fmt.Errorf("unknown subcommand %q", subcmd)
	}
}

type envelopeStream struct {
	events  <-chan protocol.Envelope
	backlog []protocol.Envelope
}

func newEnvelopeStream(events <-chan protocol.Envelope) *envelopeStream {
	return &envelopeStream{events: events}
}

func (s *envelopeStream) next(timeout time.Duration) (protocol.Envelope, error) {
	if len(s.backlog) > 0 {
		env := s.backlog[0]
		s.backlog = s.backlog[1:]
		return env, nil
	}
	select {
	case env := <-s.events:
		return env, nil
	case <-time.After(timeout):
		return protocol.Envelope{}, fmt.Errorf("timed out waiting for envelope")
	}
}

func (s *envelopeStream) push(env protocol.Envelope) {
	s.backlog = append(s.backlog, env)
}

type localClient struct {
	conn   net.Conn
	stream *envelopeStream
	errCh  chan error
}

func newLocalClient(conn net.Conn) *localClient {
	events := make(chan protocol.Envelope, 64)
	c := &localClient{conn: conn, stream: newEnvelopeStream(events), errCh: make(chan error, 1)}
	go func() {
		c.errCh <- protocol.ReadJSONL(conn, func(line []byte) error {
			env, err := protocol.DecodeEnvelope(line)
			if err != nil {
				return err
			}
			events <- env
			return nil
		})
	}()
	return c
}

func (c *localClient) initialize(sessionID string, jsonMode bool) error {
	env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "init-1", protocol.InitializeCommand{
		Command:         protocol.CommandInitialize,
		ProtocolVersion: protocol.Version,
	})
	if err != nil {
		return err
	}
	if err := protocol.WriteJSONLine(c.conn, env); err != nil {
		return err
	}
	return waitForInit(c.stream, jsonMode, "init-1")
}

func (c *localClient) execute(subcmd, message, delivery, transport, sessionID string, jsonMode bool) error {
	switch subcmd {
	case "init":
		return nil
	case "status":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "status-1", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus})
		if err != nil {
			return err
		}
		if err := protocol.WriteJSONLine(c.conn, env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, "status-1", jsonMode)
		if err != nil {
			return err
		}
		return printStatus(ack, jsonMode)
	case "sessions", "spawn", "load", "switch-transport", "kill-runtime", "attach", "takeover", "detach", "release", "inject", "pty-input", "pty-resize":
		return fmt.Errorf("%s is only supported in relay mode", subcmd)
	case "cancel":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "cancel-1", protocol.SessionCancelCommand{Command: protocol.CommandSessionCancel})
		if err != nil {
			return err
		}
		if err := protocol.WriteJSONLine(c.conn, env); err != nil {
			return err
		}
		_, err = waitForAckEnvelope(c.stream, "cancel-1", jsonMode)
		return err
	case "allow", "deny":
		decision := subcmd
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", decision+"-1", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: message, Decision: decision})
		if err != nil {
			return err
		}
		if err := protocol.WriteJSONLine(c.conn, env); err != nil {
			return err
		}
		_, err = waitForAckEnvelope(c.stream, decision+"-1", jsonMode)
		return err
	case "prompt":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: message, Delivery: delivery})
		if err != nil {
			return err
		}
		if err := protocol.WriteJSONLine(c.conn, env); err != nil {
			return err
		}
		if _, err := waitForAckEnvelope(c.stream, "prompt-1", jsonMode); err != nil {
			return err
		}
		return streamUntilComplete(c.stream, c.errCh, jsonMode)
	default:
		return fmt.Errorf("unknown subcommand %q", subcmd)
	}
}

type relayClient struct {
	conn          *websocket.Conn
	token         string
	identity      string
	debugKeysFile string
	stream        *envelopeStream
	errCh         chan error
}

func newRelayClient(conn *websocket.Conn, token, identity, debugKeysFile string) *relayClient {
	events := make(chan protocol.Envelope, 64)
	c := &relayClient{conn: conn, token: token, identity: identity, debugKeysFile: debugKeysFile, stream: newEnvelopeStream(events), errCh: make(chan error, 1)}
	go func() {
		for {
			var env protocol.Envelope
			if err := conn.ReadJSON(&env); err != nil {
				c.errCh <- err
				return
			}
			events <- env
		}
	}()
	return c
}

func (c *relayClient) authenticate(jsonMode bool) error {
	env, err := protocol.NewEnvelope(protocol.MessageCommand, "", "", c.identity, "auth-1", protocol.AuthCommand{
		Command: protocol.CommandAuth,
		Token:   c.token,
		Role:    protocol.RoleClient,
	})
	if err != nil {
		return err
	}
	if err := c.conn.WriteJSON(env); err != nil {
		return err
	}
	_, err = waitForAckEnvelope(c.stream, "auth-1", jsonMode)
	return err
}

func (c *relayClient) initialize(sessionID string, jsonMode bool) error {
	deadline := time.Now().Add(30 * time.Second)
	attempt := 0
	for {
		attempt++
		requestID := fmt.Sprintf("init-%d", attempt)
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", requestID, protocol.InitializeCommand{
			Command:         protocol.CommandInitialize,
			ProtocolVersion: protocol.Version,
		})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		if err := waitForInit(c.stream, jsonMode, requestID); err != nil {
			if strings.Contains(err.Error(), "no wrapper connected for session") && time.Now().Before(deadline) {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return err
		}
		return nil
	}
}

func (c *relayClient) execute(subcmd, message, delivery, transport, sessionID string, jsonMode bool) error {
	switch subcmd {
	case "init":
		return nil
	case "watch":
		return streamUntilInterrupt(c.stream, c.errCh, jsonMode)
	case "sessions":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, "", "", "weave-inspect", "sessions-1", protocol.ListSessionsCommand{Command: protocol.CommandRegistryListSessions})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, "sessions-1", jsonMode)
		if err != nil {
			return err
		}
		return printSessions(ack, jsonMode)
	case "status":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "status-1", protocol.SessionStatusCommand{Command: protocol.CommandSessionStatus})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, "status-1", jsonMode)
		if err != nil {
			return err
		}
		return printStatus(ack, jsonMode)
	case "spawn":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "spawn-1", protocol.SessionSpawnCommand{Command: protocol.CommandSessionSpawn, Transport: transport})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, "spawn-1", jsonMode)
		if err != nil {
			return err
		}
		return printStatus(ack, jsonMode)
	case "load":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "load-1", protocol.SessionLoadCommand{Command: protocol.CommandSessionLoad, Transport: transport})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, "load-1", jsonMode)
		if err != nil {
			return err
		}
		return printStatus(ack, jsonMode)
	case "switch-transport":
		targetTransport := message
		if targetTransport == "" {
			targetTransport = transport
		}
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "switch-transport-1", protocol.RuntimeReplaceCommand{Command: protocol.CommandRuntimeReplace, Transport: targetTransport})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, "switch-transport-1", jsonMode)
		if err != nil {
			return err
		}
		return printStatus(ack, jsonMode)
	case "kill-runtime":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "kill-runtime-1", protocol.RuntimeStopCommand{Command: protocol.CommandRuntimeStop})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, "kill-runtime-1", jsonMode)
		if err != nil {
			return err
		}
		return printStatus(ack, jsonMode)
	case "attach", "takeover":
		mode := message
		requestID := "attach-1"
		if subcmd == "takeover" {
			mode = "takeover"
			requestID = "takeover-1"
		}
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, requestID, protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: mode})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, requestID, jsonMode)
		if err != nil {
			return err
		}
		if err := printStatus(ack, jsonMode); err != nil {
			return err
		}
		if mode == "takeover" {
			if err := c.initialize(sessionID, jsonMode); err != nil {
				fmt.Fprintf(os.Stderr, "[takeover] post-attach initialize failed: %v\n", err)
			} else if rows, cols, err := c.autoResizePTY(sessionID, jsonMode); err != nil {
				fmt.Fprintf(os.Stderr, "[takeover] auto-resize failed: %v\n", err)
			} else if err := c.forceRedrawPTY(sessionID, rows, cols, jsonMode); err != nil {
				fmt.Fprintf(os.Stderr, "[takeover] redraw-resize failed: %v\n", err)
			}
			return c.interactiveTakeover(sessionID, jsonMode)
		}
		return streamUntilInterrupt(c.stream, c.errCh, jsonMode)
	case "detach", "release":
		requestID := "detach-1"
		if subcmd == "release" {
			requestID = "release-1"
		}
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, requestID, protocol.SessionDetachCommand{Command: protocol.CommandSessionDetach})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, requestID, jsonMode)
		if err != nil {
			return err
		}
		return printStatus(ack, jsonMode)
	case "inject":
		attachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: "inject"})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(attachEnv); err != nil {
			return err
		}
		if _, err := waitForAckEnvelope(c.stream, "attach-1", jsonMode); err != nil {
			return err
		}
		promptEnv, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: message, Delivery: delivery})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(promptEnv); err != nil {
			return err
		}
		if _, err := waitForAckEnvelope(c.stream, "prompt-1", jsonMode); err != nil {
			return err
		}
		streamErr := streamUntilComplete(c.stream, c.errCh, jsonMode)
		detachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "detach-1", protocol.SessionDetachCommand{Command: protocol.CommandSessionDetach})
		if err == nil {
			if err := c.conn.WriteJSON(detachEnv); err == nil {
				_, _ = waitForAckEnvelope(c.stream, "detach-1", jsonMode)
			}
		}
		return streamErr
	case "pty-input":
		return c.sendPTYInput(sessionID, []byte(message), "pty-input-1", jsonMode)
	case "pty-resize":
		parts := strings.Fields(message)
		rows, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid rows %q", parts[0])
		}
		cols, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid cols %q", parts[1])
		}
		return c.sendPTYResize(sessionID, rows, cols, "pty-resize-1", jsonMode)
	case "cancel":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "cancel-1", protocol.SessionCancelCommand{Command: protocol.CommandSessionCancel})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		_, err = waitForAckEnvelope(c.stream, "cancel-1", jsonMode)
		return err
	case "allow", "deny":
		decision := subcmd
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", decision+"-1", protocol.PermissionResponseCommand{Command: protocol.CommandSessionPermissionResponse, RequestID: message, Decision: decision})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		_, err = waitForAckEnvelope(c.stream, decision+"-1", jsonMode)
		return err
	case "prompt":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: message, Delivery: delivery})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, "prompt-1", jsonMode)
		if err != nil {
			return err
		}
		if queued, count, ok := queuedPromptAck(ack); ok {
			fmt.Fprintf(os.Stdout, "queued=%t\n", queued)
			fmt.Fprintf(os.Stdout, "queued_prompts=%d\n", count)
			return nil
		}
		return streamUntilComplete(c.stream, c.errCh, jsonMode)
	default:
		return fmt.Errorf("unknown subcommand %q", subcmd)
	}
}

func (c *relayClient) listSessionsData() ([]sessionSummary, error) {
	env, err := protocol.NewEnvelope(protocol.MessageCommand, "", "", c.identity, "sessions-tui", protocol.ListSessionsCommand{Command: protocol.CommandRegistryListSessions})
	if err != nil {
		return nil, err
	}
	if err := c.conn.WriteJSON(env); err != nil {
		return nil, err
	}
	ack, err := waitForAckEnvelope(c.stream, "sessions-tui", false)
	if err != nil {
		return nil, err
	}
	var payload protocol.AckPayload
	if err := ack.DecodePayload(&payload); err != nil {
		return nil, err
	}
	items, _ := payload.Data["sessions"].([]any)
	out := make([]sessionSummary, 0, len(items))
	for _, item := range items {
		row, _ := item.(map[string]any)
		out = append(out, parseSessionSummaryData(row))
	}
	return out, nil
}

func (c *relayClient) spawnData(sessionID, transport string) (sessionSummary, error) {
	env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "spawn-tui", protocol.SessionSpawnCommand{Command: protocol.CommandSessionSpawn, Transport: transport})
	if err != nil {
		return sessionSummary{}, err
	}
	if err := c.conn.WriteJSON(env); err != nil {
		return sessionSummary{}, err
	}
	ack, err := waitForAckEnvelope(c.stream, "spawn-tui", false)
	if err != nil {
		return sessionSummary{}, err
	}
	var payload protocol.AckPayload
	if err := ack.DecodePayload(&payload); err != nil {
		return sessionSummary{}, err
	}
	return parseSessionSummaryData(payload.Data), nil
}

func (c *relayClient) loadData(sessionID, transport string) (sessionSummary, error) {
	env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "load-tui", protocol.SessionLoadCommand{Command: protocol.CommandSessionLoad, Transport: transport})
	if err != nil {
		return sessionSummary{}, err
	}
	if err := c.conn.WriteJSON(env); err != nil {
		return sessionSummary{}, err
	}
	ack, err := waitForAckEnvelope(c.stream, "load-tui", false)
	if err != nil {
		return sessionSummary{}, err
	}
	var payload protocol.AckPayload
	if err := ack.DecodePayload(&payload); err != nil {
		return sessionSummary{}, err
	}
	return parseSessionSummaryData(payload.Data), nil
}

func (c *relayClient) releaseData(sessionID string) (sessionSummary, error) {
	env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "release-tui", protocol.SessionDetachCommand{Command: protocol.CommandSessionDetach})
	if err != nil {
		return sessionSummary{}, err
	}
	if err := c.conn.WriteJSON(env); err != nil {
		return sessionSummary{}, err
	}
	ack, err := waitForAckEnvelope(c.stream, "release-tui", false)
	if err != nil {
		return sessionSummary{}, err
	}
	var payload protocol.AckPayload
	if err := ack.DecodePayload(&payload); err != nil {
		return sessionSummary{}, err
	}
	return parseSessionSummaryData(payload.Data), nil
}

func (c *relayClient) stopData(sessionID string) (sessionSummary, error) {
	env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "stop-tui", protocol.RuntimeStopCommand{Command: protocol.CommandRuntimeStop})
	if err != nil {
		return sessionSummary{}, err
	}
	if err := c.conn.WriteJSON(env); err != nil {
		return sessionSummary{}, err
	}
	ack, err := waitForAckEnvelope(c.stream, "stop-tui", false)
	if err != nil {
		return sessionSummary{}, err
	}
	var payload protocol.AckPayload
	if err := ack.DecodePayload(&payload); err != nil {
		return sessionSummary{}, err
	}
	return parseSessionSummaryData(payload.Data), nil
}

type relayTUI struct {
	ctx         relayContext
	client      *relayClient
	sessions    []sessionSummary
	selected    int
	statusMsg   string
	prompting   bool
	promptInput string
	loading     bool
}

type tuiRefreshMsg struct {
	sessions []sessionSummary
	err      error
}

type tuiActionMsg struct {
	summary sessionSummary
	err     error
}

type tuiChildDoneMsg struct {
	err error
}

func runRelayTUI(ctx relayContext) error {
	client, err := dialRelayClient(ctx)
	if err != nil {
		return err
	}
	defer client.conn.Close()

	model := &relayTUI{ctx: ctx, client: client}
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func dialRelayClient(ctx relayContext) (*relayClient, error) {
	conn, _, err := websocket.DefaultDialer.Dial(ctx.RelayURL, nil)
	if err != nil {
		return nil, err
	}
	client := newRelayClient(conn, ctx.Token, ctx.Identity, ctx.DebugKeysFile)
	if err := client.authenticate(false); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}

func (t *relayTUI) Init() tea.Cmd {
	return t.refreshCmd()
}

func (t *relayTUI) refreshCmd() tea.Cmd {
	t.loading = true
	return func() tea.Msg {
		sessions, err := t.client.listSessionsData()
		return tuiRefreshMsg{sessions: sessions, err: err}
	}
}

func (t *relayTUI) updateSessions(sessions []sessionSummary) {
	t.sessions = sessions
	if len(t.sessions) == 0 {
		t.selected = 0
		return
	}
	if t.ctx.SessionID != "" {
		for i, session := range t.sessions {
			if session.SessionID == t.ctx.SessionID {
				t.selected = i
				return
			}
		}
	}
	if t.selected >= len(t.sessions) {
		t.selected = len(t.sessions) - 1
	}
	if t.selected < 0 {
		t.selected = 0
	}
	t.ctx.SessionID = t.sessions[t.selected].SessionID
}

func (t *relayTUI) current() (sessionSummary, bool) {
	if len(t.sessions) == 0 || t.selected < 0 || t.selected >= len(t.sessions) {
		return sessionSummary{}, false
	}
	return t.sessions[t.selected], true
}

func (t *relayTUI) setSelected(idx int) {
	if len(t.sessions) == 0 {
		t.selected = 0
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(t.sessions) {
		idx = len(t.sessions) - 1
	}
	t.selected = idx
	t.ctx.SessionID = t.sessions[t.selected].SessionID
	_ = saveRelayContext(t.ctx)
}

func (t *relayTUI) actionCmd(fn func() (sessionSummary, error)) tea.Cmd {
	t.loading = true
	return func() tea.Msg {
		summary, err := fn()
		return tuiActionMsg{summary: summary, err: err}
	}
}

func (t *relayTUI) childCmd(args ...string) tea.Cmd {
	if err := saveRelayContext(t.ctx); err != nil {
		return func() tea.Msg { return tuiChildDoneMsg{err: err} }
	}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return tuiChildDoneMsg{err: err}
	})
}

func (t *relayTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tuiRefreshMsg:
		t.loading = false
		if msg.err != nil {
			t.statusMsg = msg.err.Error()
			return t, nil
		}
		t.updateSessions(msg.sessions)
		if t.statusMsg == "" {
			t.statusMsg = "sessions refreshed"
		}
		return t, saveContextCmd(t.ctx)
	case tuiActionMsg:
		t.loading = false
		if msg.err != nil {
			t.statusMsg = msg.err.Error()
			return t, nil
		}
		if msg.summary.SessionID != "" {
			t.ctx.SessionID = msg.summary.SessionID
		}
		t.statusMsg = "action complete"
		return t, tea.Batch(saveContextCmd(t.ctx), t.refreshCmd())
	case tuiChildDoneMsg:
		if msg.err != nil {
			t.statusMsg = msg.err.Error()
		} else {
			t.statusMsg = "returned from child command"
		}
		client, err := dialRelayClient(t.ctx)
		if err != nil {
			t.statusMsg = err.Error()
			return t, nil
		}
		if t.client != nil && t.client.conn != nil {
			_ = t.client.conn.Close()
		}
		t.client = client
		return t, t.refreshCmd()
	case tea.KeyMsg:
		if t.loading {
			switch msg.String() {
			case "ctrl+c", "q":
				_ = saveRelayContext(t.ctx)
				return t, tea.Quit
			default:
				return t, nil
			}
		}
		if t.prompting {
			switch msg.String() {
			case "esc":
				t.prompting = false
				t.promptInput = ""
				t.statusMsg = "prompt cancelled"
				return t, nil
			case "enter":
				prompt := strings.TrimSpace(t.promptInput)
				t.prompting = false
				t.promptInput = ""
				if prompt == "" {
					t.statusMsg = "prompt cancelled"
					return t, nil
				}
				return t, t.childCmd("relay", "prompt", prompt)
			case "backspace":
				if len(t.promptInput) > 0 {
					t.promptInput = t.promptInput[:len(t.promptInput)-1]
				}
				return t, nil
			default:
				if msg.Type == tea.KeyRunes {
					t.promptInput += string(msg.Runes)
				}
				return t, nil
			}
		}

		switch msg.String() {
		case "ctrl+c", "q":
			_ = saveRelayContext(t.ctx)
			return t, tea.Quit
		case "up", "k":
			t.statusMsg = ""
			t.setSelected(t.selected - 1)
			return t, nil
		case "down", "j":
			t.statusMsg = ""
			t.setSelected(t.selected + 1)
			return t, nil
		case "r":
			t.statusMsg = ""
			return t, t.refreshCmd()
		case "n":
			t.statusMsg = ""
			sessionID := fmt.Sprintf("tui-%s", time.Now().UTC().Format("20060102-150405"))
			return t, t.actionCmd(func() (sessionSummary, error) {
				return t.client.spawnData(sessionID, "")
			})
		case "l":
			current, ok := t.current()
			if !ok {
				t.statusMsg = "no selected session"
				return t, nil
			}
			return t, t.actionCmd(func() (sessionSummary, error) {
				return t.client.loadData(current.SessionID, "")
			})
		case "x":
			current, ok := t.current()
			if !ok {
				t.statusMsg = "no selected session"
				return t, nil
			}
			return t, t.actionCmd(func() (sessionSummary, error) {
				return t.client.stopData(current.SessionID)
			})
		case "e":
			current, ok := t.current()
			if !ok {
				t.statusMsg = "no selected session"
				return t, nil
			}
			return t, t.actionCmd(func() (sessionSummary, error) {
				return t.client.releaseData(current.SessionID)
			})
		case "t":
			current, ok := t.current()
			if !ok {
				t.statusMsg = "no selected session"
				return t, nil
			}
			t.ctx.SessionID = current.SessionID
			return t, t.childCmd("relay", "takeover")
		case "p":
			current, ok := t.current()
			if !ok {
				t.statusMsg = "no selected session"
				return t, nil
			}
			t.ctx.SessionID = current.SessionID
			t.prompting = true
			t.promptInput = ""
			t.statusMsg = "enter prompt, then press Enter"
			return t, saveContextCmd(t.ctx)
		case "enter":
			if current, ok := t.current(); ok {
				t.ctx.SessionID = current.SessionID
				t.statusMsg = "selected session saved"
				return t, saveContextCmd(t.ctx)
			}
			return t, nil
		}
	}
	return t, nil
}

func saveContextCmd(ctx relayContext) tea.Cmd {
	return func() tea.Msg {
		_ = saveRelayContext(ctx)
		return nil
	}
}

func (t *relayTUI) View() string {
	var b strings.Builder
	b.WriteString("weave-inspect relay tui\n")
	fmt.Fprintf(&b, "relay=%s\nidentity=%s\nsession=%s\n", t.ctx.RelayURL, t.ctx.Identity, t.ctx.SessionID)
	b.WriteString("keys: ↑/↓ or j/k move  n spawn  l load  x stop  e release  t takeover  p prompt  r refresh  q quit\n")
	if t.loading {
		b.WriteString("status: loading...\n")
	} else if t.statusMsg != "" {
		fmt.Fprintf(&b, "status: %s\n", t.statusMsg)
	} else {
		b.WriteString("status: ready\n")
	}
	b.WriteString(strings.Repeat("-", 100))
	b.WriteString("\n")
	if len(t.sessions) == 0 {
		b.WriteString("(no sessions; press n to spawn one)\n")
	} else {
		for i, session := range t.sessions {
			cursor := " "
			if i == t.selected {
				cursor = ">"
			}
			fmt.Fprintf(&b, "%s %-28s %-7s %-4s attached=%-10s queued=%d perms=%d\n",
				cursor,
				session.SessionID,
				session.State,
				session.RuntimeTransport,
				session.AttachedMode,
				session.QueuedPrompts,
				session.PendingPermissions,
			)
		}
	}
	b.WriteString(strings.Repeat("-", 100))
	b.WriteString("\n")
	if session, ok := t.current(); ok {
		fmt.Fprintf(&b, "selected: %s\n", session.SessionID)
		fmt.Fprintf(&b, "  runtime=%s/%s wrapper_connected=%t\n", session.RuntimeKind, session.RuntimeTransport, session.WrapperConnected)
		fmt.Fprintf(&b, "  attached_client=%s attached_mode=%s return_transport=%s\n", session.AttachedClientID, session.AttachedMode, session.AttachedReturnMode)
		fmt.Fprintf(&b, "  phase=%s pty=%dx%d updated=%s\n", session.Phase, session.PTYRows, session.PTYCols, session.UpdatedAt)
	}
	if t.prompting {
		b.WriteString("\n")
		fmt.Fprintf(&b, "prompt> %s\n", t.promptInput)
		b.WriteString("(Enter to send, Esc to cancel)\n")
	}
	return b.String()
}

func (c *relayClient) sendPTYResize(sessionID string, rows, cols int, requestID string, jsonMode bool) error {
	env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, requestID, protocol.PTYResizeCommand{
		Command: protocol.CommandPTYResize,
		Rows:    rows,
		Cols:    cols,
	})
	if err != nil {
		return err
	}
	if err := c.conn.WriteJSON(env); err != nil {
		return err
	}
	_, err = waitForAckEnvelope(c.stream, requestID, jsonMode)
	return err
}

func (c *relayClient) autoResizePTY(sessionID string, jsonMode bool) (int, int, error) {
	rows, cols, ok := terminalSize()
	if !ok {
		return 0, 0, nil
	}
	if err := c.sendPTYResize(sessionID, rows, cols, "pty-resize-auto", jsonMode); err != nil {
		return 0, 0, err
	}
	return rows, cols, nil
}

func (c *relayClient) forceRedrawPTY(sessionID string, rows, cols int, jsonMode bool) error {
	if rows <= 0 || cols <= 0 {
		return nil
	}
	bumpRows, bumpCols := rows, cols
	if rows < 9999 {
		bumpRows = rows + 1
	} else if cols < 9999 {
		bumpCols = cols + 1
	} else {
		return nil
	}
	if err := c.sendPTYResize(sessionID, bumpRows, bumpCols, "pty-resize-redraw-bump", jsonMode); err != nil {
		return err
	}
	return c.sendPTYResize(sessionID, rows, cols, "pty-resize-redraw-restore", jsonMode)
}

func (c *relayClient) sendPTYInput(sessionID string, data []byte, requestID string, jsonMode bool) error {
	if len(data) == 0 {
		return nil
	}
	env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, requestID, protocol.PTYInputCommand{
		Command: protocol.CommandPTYInput,
		Data:    base64.StdEncoding.EncodeToString(data),
	})
	if err != nil {
		return err
	}
	if err := c.conn.WriteJSON(env); err != nil {
		return err
	}
	_, err = waitForAckEnvelope(c.stream, requestID, jsonMode)
	return err
}

func (c *relayClient) interactiveTakeover(sessionID string, jsonMode bool) error {
	fmt.Fprintln(os.Stderr, "[takeover] interactive mode active (Ctrl-] or ~. at line start to disconnect)")
	restore, err := makeRawTerminal(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[takeover] raw mode unavailable: %v\n", err)
	}
	if restore != nil {
		defer restore()
	}

	var debugWriter io.WriteCloser
	if c.debugKeysFile != "" {
		f, fileErr := os.OpenFile(c.debugKeysFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if fileErr != nil {
			fmt.Fprintf(os.Stderr, "[takeover] failed to open debug key log %s: %v\n", c.debugKeysFile, fileErr)
		} else {
			debugWriter = f
			fmt.Fprintf(os.Stderr, "[takeover] logging raw stdin bytes to %s\n", c.debugKeysFile)
			defer debugWriter.Close()
		}
	}

	inputCh := make(chan takeoverInputEvent, 64)
	errorCh := make(chan error, 1)
	go readTakeoverInput(os.Stdin, inputCh, errorCh, debugWriter)

	requestSeq := 0
	for {
		if len(c.stream.backlog) == 0 {
			select {
			case err := <-errorCh:
				if err != nil {
					return err
				}
				return nil
			case err := <-c.errCh:
				if err != nil {
					return err
				}
				return nil
			case evt, ok := <-inputCh:
				if !ok {
					return nil
				}
				if evt.disconnect {
					fmt.Fprintln(os.Stderr, "\n[takeover] disconnect requested")
					return nil
				}
				forward := append([]byte(nil), evt.data...)
				disconnect := false
				for {
					select {
					case nextEvt, ok := <-inputCh:
						if !ok {
							ok = false
							disconnect = false
							goto flushForward
						}
						if nextEvt.disconnect {
							disconnect = true
							goto flushForward
						}
						forward = append(forward, nextEvt.data...)
					default:
						goto flushForward
					}
				}
			flushForward:
				if len(forward) > 0 {
					requestSeq++
					if err := c.sendPTYInput(sessionID, forward, fmt.Sprintf("pty-input-live-%d", requestSeq), jsonMode); err != nil {
						return err
					}
				}
				if disconnect {
					fmt.Fprintln(os.Stderr, "\n[takeover] disconnect requested")
					return nil
				}
				continue
			default:
			}
		}

		env, err := c.stream.next(100 * time.Millisecond)
		if err != nil {
			if strings.Contains(err.Error(), "timed out waiting for envelope") {
				continue
			}
			select {
			case err := <-c.errCh:
				if err != nil {
					return err
				}
				return nil
			default:
				return err
			}
		}
		if jsonMode {
			_ = protocol.WriteJSONLine(os.Stdout, env)
			continue
		}
		_, _, eventErr := printEventEnvelope(env)
		if eventErr != nil {
			return eventErr
		}
	}
}

func terminalSize() (rows, cols int, ok bool) {
	for _, fd := range []uintptr{os.Stdout.Fd(), os.Stdin.Fd(), os.Stderr.Fd()} {
		ws, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
		if err != nil || ws == nil {
			continue
		}
		if ws.Row > 0 && ws.Col > 0 {
			return int(ws.Row), int(ws.Col), true
		}
	}
	rows, rowsErr := strconv.Atoi(strings.TrimSpace(os.Getenv("LINES")))
	cols, colsErr := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS")))
	if rowsErr == nil && colsErr == nil && rows > 0 && cols > 0 {
		return rows, cols, true
	}
	return 0, 0, false
}

func makeRawTerminal(file *os.File) (func(), error) {
	if file == nil {
		return nil, nil
	}
	fd := int(file.Fd())
	orig, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}
	raw := *orig
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return nil, err
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, unix.TCSETS, orig)
	}, nil
}

type takeoverInputEvent struct {
	data       []byte
	disconnect bool
}

var ctrlRightBracketSequences = [][]byte{
	{0x1d},                               // raw Ctrl-]
	{0x1b, '[', '9', '3', ';', '5', 'u'}, // CSI-u
	{0x1b, '[', '2', '7', ';', '5', ';', '9', '3', '~'}, // xterm modifyOtherKeys
}

func logTakeoverByte(w io.Writer, b byte, note string) {
	if w == nil {
		return
	}
	printable := '.'
	if b >= 32 && b <= 126 {
		printable = rune(b)
	}
	_, _ = fmt.Fprintf(w, "%s hex=%02x dec=%d printable=%q note=%s\n", time.Now().UTC().Format(time.RFC3339Nano), b, b, printable, note)
}

func logTakeoverBytes(w io.Writer, data []byte, note string) {
	for _, b := range data {
		logTakeoverByte(w, b, note)
	}
}

func isPrefixBytes(have, want []byte) bool {
	if len(have) > len(want) {
		return false
	}
	for i := range have {
		if have[i] != want[i] {
			return false
		}
	}
	return true
}

func matchesDisconnectSequence(data []byte) (string, bool) {
	for _, seq := range ctrlRightBracketSequences {
		if bytes.Equal(data, seq) {
			switch {
			case len(seq) == 1:
				return "ctrl-right-bracket-disconnect", true
			case bytes.HasSuffix(seq, []byte{'u'}):
				return "ctrl-right-bracket-csiu-disconnect", true
			default:
				return "ctrl-right-bracket-modifyotherkeys-disconnect", true
			}
		}
	}
	return "", false
}

func hasDisconnectPrefix(data []byte) bool {
	for _, seq := range ctrlRightBracketSequences {
		if len(data) > 1 && isPrefixBytes(data, seq) {
			return true
		}
	}
	return false
}

func readTakeoverInput(r io.Reader, out chan<- takeoverInputEvent, errCh chan<- error, debug io.Writer) {
	defer close(out)
	buf := make([]byte, 1)
	atLineStart := true
	pendingTilde := false
	pendingEscape := make([]byte, 0, 16)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			b := buf[0]
			logTakeoverByte(debug, b, "read")

			if len(pendingEscape) > 0 || b == 0x1b {
				pendingEscape = append(pendingEscape, b)
				if note, ok := matchesDisconnectSequence(pendingEscape); ok {
					logTakeoverBytes(debug, pendingEscape, note)
					out <- takeoverInputEvent{disconnect: true}
					pendingEscape = pendingEscape[:0]
					continue
				}
				if hasDisconnectPrefix(pendingEscape) {
					continue
				}
				logTakeoverBytes(debug, pendingEscape, "flush-escape")
				out <- takeoverInputEvent{data: append([]byte(nil), pendingEscape...)}
				atLineStart = pendingEscape[len(pendingEscape)-1] == '\r' || pendingEscape[len(pendingEscape)-1] == '\n'
				pendingEscape = pendingEscape[:0]
				continue
			}

			if pendingTilde {
				pendingTilde = false
				if b == '.' {
					logTakeoverByte(debug, b, "tilde-dot-disconnect")
					out <- takeoverInputEvent{disconnect: true}
					continue
				}
				logTakeoverByte(debug, b, "flush-pending-tilde")
				out <- takeoverInputEvent{data: []byte{'~', b}}
				atLineStart = b == '\r' || b == '\n'
				continue
			}
			if b == 0x1d {
				logTakeoverByte(debug, b, "ctrl-right-bracket-disconnect")
				out <- takeoverInputEvent{disconnect: true}
				continue
			}
			if atLineStart && b == '~' {
				logTakeoverByte(debug, b, "pending-tilde")
				pendingTilde = true
				atLineStart = false
				continue
			}
			logTakeoverByte(debug, b, "forward")
			out <- takeoverInputEvent{data: []byte{b}}
			atLineStart = b == '\r' || b == '\n'
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(pendingEscape) > 0 {
					logTakeoverBytes(debug, pendingEscape, "flush-escape-eof")
					out <- takeoverInputEvent{data: append([]byte(nil), pendingEscape...)}
				}
				return
			}
			errCh <- err
			return
		}
	}
}

func waitForInit(stream *envelopeStream, jsonMode bool, requestID string) error {
	deadline := time.Now().Add(10 * time.Second)
	seenAck := false
	seenReady := false
	var skipped []protocol.Envelope
	defer func() {
		for i := len(skipped) - 1; i >= 0; i-- {
			stream.push(skipped[i])
		}
	}()
	for !(seenAck && seenReady) {
		env, err := stream.next(time.Until(deadline))
		if err != nil {
			return fmt.Errorf("timed out waiting for initialize")
		}
		if jsonMode {
			_ = protocol.WriteJSONLine(os.Stdout, env)
		}
		if env.ID == requestID && env.Type == protocol.MessageError {
			var payload protocol.ErrorPayload
			if err := env.DecodePayload(&payload); err != nil {
				return err
			}
			return errors.New(payload.Error)
		}
		if env.Type == protocol.MessageAck && env.ID == requestID {
			seenAck = true
			continue
		}
		if env.Type == protocol.MessageEvent {
			var evt protocol.AgentReadyEvent
			if err := env.DecodePayload(&evt); err == nil && evt.Event == protocol.EventSessionAgentReady {
				seenReady = true
				continue
			}
		}
		skipped = append(skipped, env)
	}
	return nil
}

func waitForAckEnvelope(stream *envelopeStream, id string, jsonMode bool) (protocol.Envelope, error) {
	deadline := time.Now().Add(10 * time.Second)
	var skipped []protocol.Envelope
	defer func() {
		for i := len(skipped) - 1; i >= 0; i-- {
			stream.push(skipped[i])
		}
	}()
	for {
		env, err := stream.next(time.Until(deadline))
		if err != nil {
			return protocol.Envelope{}, fmt.Errorf("timed out waiting for %s", id)
		}
		if jsonMode {
			_ = protocol.WriteJSONLine(os.Stdout, env)
		}
		if env.ID != id {
			skipped = append(skipped, env)
			continue
		}
		switch env.Type {
		case protocol.MessageAck:
			return env, nil
		case protocol.MessageError:
			var payload protocol.ErrorPayload
			if err := env.DecodePayload(&payload); err != nil {
				return protocol.Envelope{}, err
			}
			return protocol.Envelope{}, errors.New(payload.Error)
		default:
			skipped = append(skipped, env)
		}
	}
}

func printStatus(env protocol.Envelope, jsonMode bool) error {
	if jsonMode {
		return nil
	}
	var ack protocol.AckPayload
	if err := env.DecodePayload(&ack); err != nil {
		return err
	}
	sessionMap, _ := ack.Data["session"].(map[string]any)
	runtimeMap, _ := ack.Data["runtime"].(map[string]any)
	fmt.Fprintf(os.Stdout, "session_id=%s\n", stringValue(sessionMap["id"]))
	fmt.Fprintf(os.Stdout, "runtime_id=%s\n", stringValue(runtimeMap["id"]))
	fmt.Fprintf(os.Stdout, "runtime_kind=%s\n", stringValue(runtimeMap["kind"]))
	fmt.Fprintf(os.Stdout, "runtime_transport=%s\n", stringValue(runtimeMap["transport"]))
	if persisted := stringValue(ack.Data["persisted_session_handle"]); persisted != "" {
		fmt.Fprintf(os.Stdout, "persisted_session_handle=%s\n", persisted)
	}
	if state := stringValue(ack.Data["state"]); state != "" {
		fmt.Fprintf(os.Stdout, "state=%s\n", state)
	}
	if phase := stringValue(ack.Data["phase"]); phase != "" {
		fmt.Fprintf(os.Stdout, "phase=%s\n", phase)
	}
	if wrapperConnected, ok := ack.Data["wrapper_connected"].(bool); ok {
		fmt.Fprintf(os.Stdout, "wrapper_connected=%t\n", wrapperConnected)
	}
	if attachment, ok := ack.Data["attachment"].(map[string]any); ok && len(attachment) > 0 {
		fmt.Fprintf(os.Stdout, "attached_client_id=%s\n", stringValue(attachment["client_id"]))
		fmt.Fprintf(os.Stdout, "attached_mode=%s\n", stringValue(attachment["mode"]))
		if returnTransport := stringValue(attachment["return_transport"]); returnTransport != "" {
			fmt.Fprintf(os.Stdout, "attached_return_transport=%s\n", returnTransport)
		}
	}
	if queuedPrompts, ok := ack.Data["queued_prompts"].(float64); ok {
		fmt.Fprintf(os.Stdout, "queued_prompts=%d\n", int(queuedPrompts))
	} else if queuedPrompts, ok := ack.Data["queued_prompts"].(int); ok {
		fmt.Fprintf(os.Stdout, "queued_prompts=%d\n", queuedPrompts)
	}
	if ptyRows, ok := ack.Data["pty_rows"].(float64); ok {
		fmt.Fprintf(os.Stdout, "pty_rows=%d\n", int(ptyRows))
	}
	if ptyCols, ok := ack.Data["pty_cols"].(float64); ok {
		fmt.Fprintf(os.Stdout, "pty_cols=%d\n", int(ptyCols))
	}
	if pending, ok := ack.Data["pending_permissions"].([]any); ok {
		fmt.Fprintf(os.Stdout, "pending_permissions=%d\n", len(pending))
		for _, item := range pending {
			row, _ := item.(map[string]any)
			fmt.Fprintf(os.Stdout, "pending_permission_id=%s kind=%s title=%s\n", stringValue(row["id"]), stringValue(row["kind"]), stringValue(row["title"]))
		}
	}
	if updatedAt := stringValue(ack.Data["updated_at"]); updatedAt != "" {
		fmt.Fprintf(os.Stdout, "updated_at=%s\n", updatedAt)
	}
	return nil
}

func printSessions(env protocol.Envelope, jsonMode bool) error {
	if jsonMode {
		return nil
	}
	var ack protocol.AckPayload
	if err := env.DecodePayload(&ack); err != nil {
		return err
	}
	items, _ := ack.Data["sessions"].([]any)
	if len(items) == 0 {
		fmt.Fprintln(os.Stdout, "no sessions")
		return nil
	}
	for _, item := range items {
		row, _ := item.(map[string]any)
		sessionMap, _ := row["session"].(map[string]any)
		runtimeMap, _ := row["runtime"].(map[string]any)
		pendingCount := 0
		if pending, ok := row["pending_permissions"].([]any); ok {
			pendingCount = len(pending)
		}
		queuedCount := 0
		if queued, ok := row["queued_prompts"].(float64); ok {
			queuedCount = int(queued)
		} else if queued, ok := row["queued_prompts"].(int); ok {
			queuedCount = queued
		}
		attachmentID := ""
		attachmentMode := ""
		if attachment, ok := row["attachment"].(map[string]any); ok {
			attachmentID = stringValue(attachment["client_id"])
			attachmentMode = stringValue(attachment["mode"])
		}
		fmt.Fprintf(os.Stdout, "session_id=%s runtime_id=%s state=%s phase=%s wrapper_connected=%v queued_prompts=%d pending_permissions=%d attached_client_id=%s attached_mode=%s\n",
			stringValue(sessionMap["id"]),
			stringValue(runtimeMap["id"]),
			stringValue(row["state"]),
			stringValue(row["phase"]),
			row["wrapper_connected"],
			queuedCount,
			pendingCount,
			attachmentID,
			attachmentMode,
		)
		if persisted := stringValue(row["persisted_session_handle"]); persisted != "" {
			fmt.Fprintf(os.Stdout, "  persisted_session_handle=%s\n", persisted)
		}
		if updatedAt := stringValue(row["updated_at"]); updatedAt != "" {
			fmt.Fprintf(os.Stdout, "  updated_at=%s\n", updatedAt)
		}
	}
	return nil
}

func queuedPromptAck(env protocol.Envelope) (bool, int, bool) {
	var ack protocol.AckPayload
	if err := env.DecodePayload(&ack); err != nil {
		return false, 0, false
	}
	queued, ok := ack.Data["queued"].(bool)
	if !ok || !queued {
		return false, 0, false
	}
	count := 0
	if queuedCount, ok := ack.Data["queued_prompts"].(float64); ok {
		count = int(queuedCount)
	} else if queuedCount, ok := ack.Data["queued_prompts"].(int); ok {
		count = queuedCount
	}
	return true, count, true
}

func streamUntilComplete(stream *envelopeStream, errCh <-chan error, jsonMode bool) error {
	for {
		if len(stream.backlog) == 0 {
			select {
			case err := <-errCh:
				if err != nil {
					return err
				}
				return nil
			default:
			}
		}
		env, err := stream.next(30 * time.Second)
		if err != nil {
			select {
			case err := <-errCh:
				if err != nil {
					return err
				}
				return nil
			default:
				return err
			}
		}
		if jsonMode {
			_ = protocol.WriteJSONLine(os.Stdout, env)
			continue
		}
		done, handled, eventErr := printEventEnvelope(env)
		if eventErr != nil {
			return eventErr
		}
		if !handled {
			continue
		}
		if done {
			return nil
		}
	}
}

func streamUntilInterrupt(stream *envelopeStream, errCh <-chan error, jsonMode bool) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	for {
		if len(stream.backlog) == 0 {
			select {
			case err := <-errCh:
				if err != nil {
					return err
				}
				return nil
			case <-sigCh:
				return nil
			default:
			}
		}
		env, err := stream.next(1 * time.Second)
		if err != nil {
			select {
			case <-sigCh:
				return nil
			case err := <-errCh:
				if err != nil {
					return err
				}
				return nil
			default:
				continue
			}
		}
		if jsonMode {
			_ = protocol.WriteJSONLine(os.Stdout, env)
			continue
		}
		_, _, updateErr := printEventEnvelope(env)
		if updateErr != nil {
			return updateErr
		}
	}
}

func printEventEnvelope(env protocol.Envelope) (bool, bool, error) {
	if env.Type != protocol.MessageEvent {
		return false, false, nil
	}
	var meta struct {
		Event string `json:"event"`
	}
	if err := env.DecodePayload(&meta); err != nil {
		return false, false, nil
	}
	switch meta.Event {
	case protocol.EventSessionUpdate:
		var evt protocol.SessionUpdateEvent
		if err := env.DecodePayload(&evt); err != nil {
			return false, false, nil
		}
		done, err := printUpdate(evt.Update)
		return done, true, err
	case protocol.EventPTYOutput:
		var evt protocol.PTYOutputEvent
		if err := env.DecodePayload(&evt); err != nil {
			return false, false, nil
		}
		data, err := base64.StdEncoding.DecodeString(evt.Data)
		if err != nil {
			return false, true, err
		}
		fmt.Fprint(os.Stdout, string(data))
		return false, true, nil
	default:
		return false, false, nil
	}
}

func printUpdate(update protocol.SessionUpdate) (bool, error) {
	switch update.Kind {
	case protocol.UpdateMessageDelta:
		fmt.Fprint(os.Stdout, update.Delta)
	case protocol.UpdateMessageComplete:
		if update.Message != "" {
			fmt.Fprintln(os.Stdout)
		}
	case protocol.UpdateToolBegin:
		fmt.Fprintf(os.Stderr, "\n[tool start] %s\n", update.ToolName)
	case protocol.UpdateToolEnd:
		fmt.Fprintf(os.Stderr, "\n[tool end] %s\n", update.ToolName)
	case protocol.UpdatePermissionRequest:
		title := update.Message
		if update.Permission != nil && update.Permission.Title != "" {
			title = update.Permission.Title
		}
		fmt.Fprintf(os.Stderr, "\n[permission request] id=%s title=%s\n", update.RequestID, title)
	case protocol.UpdatePermissionResolved:
		fmt.Fprintf(os.Stderr, "\n[permission resolved] id=%s decision=%s\n", update.RequestID, update.Decision)
	case protocol.UpdateStatus:
		if update.Phase != "" {
			fmt.Fprintf(os.Stderr, "\n[status] %s\n", update.Phase)
		}
		if action := stringValue(update.Details["attachment_action"]); action != "" {
			attachment := mapValue(update.Details["attachment"])
			clientID := stringValue(update.Details["attachment_client_id"])
			if clientID == "" {
				clientID = stringValue(attachment["client_id"])
			}
			mode := stringValue(attachment["mode"])
			if previousMode := stringValue(update.Details["previous_mode"]); previousMode != "" {
				fmt.Fprintf(os.Stderr, "[attachment] action=%s client=%s mode=%s previous_mode=%s\n", action, clientID, mode, previousMode)
			} else {
				fmt.Fprintf(os.Stderr, "[attachment] action=%s client=%s mode=%s\n", action, clientID, mode)
			}
		}
		if queueAction := stringValue(update.Details["takeover_queue_action"]); queueAction != "" {
			queuedCount := 0
			if queued, ok := update.Details["queued_prompts"].(float64); ok {
				queuedCount = int(queued)
			} else if queued, ok := update.Details["queued_prompts"].(int); ok {
				queuedCount = queued
			}
			fmt.Fprintf(os.Stderr, "[takeover queue] action=%s queued_prompts=%d\n", queueAction, queuedCount)
		}
	case protocol.UpdateError:
		return false, errors.New(update.Message)
	case protocol.UpdateComplete:
		fmt.Fprintln(os.Stdout)
		return true, nil
	}
	return false, nil
}

func shouldInitializeRelay(subcmd string) bool {
	switch subcmd {
	case "init", "watch", "inject", "pty-input", "pty-resize", "prompt", "cancel", "allow", "deny", "attach":
		return true
	default:
		return false
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
