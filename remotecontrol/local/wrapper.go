package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

type Config struct {
	SocketPath      string
	SessionID       string
	PiBin           string
	PiArgs          []string
	Env             map[string]string
	StartupTimeout  time.Duration
	NoDefaultPiArgs bool
	Logger          *log.Logger
}

type Wrapper struct {
	cfg Config

	runtimeID string

	listener *net.UnixListener

	peersMu sync.Mutex
	peers   map[int64]peer
	nextID  int64

	piInMu sync.Mutex
	piIn   io.WriteCloser

	pendingMu sync.Mutex
	pending   map[string]chan piResponse

	bootstrapState map[string]any

	closed chan struct{}
}

type piResponse struct {
	ID      string
	Command string
	Success bool
	Error   string
	Data    map[string]any
}

type peer interface {
	writeEnvelope(protocol.Envelope) error
	setInitialized(bool)
	initialized() bool
}

type localPeer struct {
	conn net.Conn
	mu   sync.Mutex
	init bool
}

type relayPeer struct {
	conn *websocket.Conn
	mu   sync.Mutex
	init bool
}

func NewWrapper(cfg Config) *Wrapper {
	if cfg.SocketPath == "" {
		cfg.SocketPath = filepath.Join(os.TempDir(), "weave-local.sock")
	}
	if cfg.SessionID == "" {
		cfg.SessionID = "local"
	}
	if cfg.PiBin == "" {
		cfg.PiBin = "pi"
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = 15 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	return &Wrapper{
		cfg:       cfg,
		runtimeID: fmt.Sprintf("rt-%d", time.Now().UTC().UnixNano()),
		peers:     make(map[int64]peer),
		pending:   make(map[string]chan piResponse),
		closed:    make(chan struct{}),
	}
}

func (w *Wrapper) Run(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(w.cfg.SocketPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(w.cfg.SocketPath)
	addr := &net.UnixAddr{Name: w.cfg.SocketPath, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		return err
	}
	w.listener = ln
	defer func() {
		_ = ln.Close()
		_ = os.Remove(w.cfg.SocketPath)
		close(w.closed)
	}()

	procErr, cleanup, err := w.startRuntime(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.acceptLoop(ctx)
	}()

	select {
	case err := <-procErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			w.broadcastUpdate(protocol.SessionUpdate{Kind: protocol.UpdateError, Message: "pi process exited", Details: map[string]any{"error": err.Error()}})
			return err
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Wrapper) RunRelay(ctx context.Context, relayURL, token string) error {
	procErr, cleanup, err := w.startRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup()
		close(w.closed)
	}()

	conn, _, err := websocket.DefaultDialer.Dial(relayURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	rpeer := &relayPeer{conn: conn}
	peerID := w.registerPeer(rpeer)
	defer w.unregisterPeer(peerID)

	if err := w.authRelay(conn, token); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.readRelay(ctx, rpeer)
	}()

	select {
	case err := <-procErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			w.broadcastUpdate(protocol.SessionUpdate{Kind: protocol.UpdateError, Message: "pi process exited", Details: map[string]any{"error": err.Error()}})
			return err
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Wrapper) authRelay(conn *websocket.Conn, token string) error {
	env, err := protocol.NewEnvelope(protocol.MessageCommand, w.cfg.SessionID, w.runtimeID, "weave-wrapper", "auth-wrapper", protocol.AuthCommand{
		Command: protocol.CommandAuth,
		Token:   token,
		Role:    protocol.RoleWrapper,
	})
	if err != nil {
		return err
	}
	if err := conn.WriteJSON(env); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(w.cfg.StartupTimeout))
	defer conn.SetReadDeadline(time.Time{})
	var ack protocol.Envelope
	if err := conn.ReadJSON(&ack); err != nil {
		return err
	}
	if ack.Type != protocol.MessageAck || ack.ID != "auth-wrapper" {
		return fmt.Errorf("unexpected relay auth response: type=%s id=%s", ack.Type, ack.ID)
	}
	return nil
}

func (w *Wrapper) readRelay(ctx context.Context, p *relayPeer) error {
	for {
		var env protocol.Envelope
		if err := p.conn.ReadJSON(&env); err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			return err
		}
		if env.Type != protocol.MessageCommand {
			continue
		}
		if err := w.handleCommand(ctx, p, env); err != nil {
			return err
		}
	}
}

func (w *Wrapper) startRuntime(ctx context.Context) (<-chan error, func(), error) {
	cmd, stdout, stderr, err := w.startPi(ctx)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}

	procErr := make(chan error, 1)
	go func() {
		procErr <- cmd.Wait()
	}()
	go w.logStderr(stderr)
	go w.readPiOutput(stdout)

	state, err := w.bootstrap(ctx)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	w.bootstrapState = state
	return procErr, cleanup, nil
}

func (w *Wrapper) startPi(ctx context.Context) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
	args := append([]string{}, w.cfg.PiArgs...)
	if !w.cfg.NoDefaultPiArgs {
		if !containsArg(args, "--mode") {
			args = append(args, "--mode", "rpc")
		}
		if !containsAny(args, "--no-session", "--session", "--session-dir", "-c", "--continue", "-r", "--resume") {
			args = append(args, "--no-session")
		}
	}

	cmd := exec.CommandContext(ctx, w.cfg.PiBin, args...)
	if len(w.cfg.Env) > 0 {
		env := os.Environ()
		for key, value := range w.cfg.Env {
			env = append(env, key+"="+value)
		}
		cmd.Env = env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	w.piIn = stdin
	return cmd, stdout, stderr, nil
}

func (w *Wrapper) bootstrap(ctx context.Context) (map[string]any, error) {
	resp, err := w.sendPICommand(ctx, map[string]any{"id": "bootstrap-state", "type": "get_state"})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		if resp.Error == "" {
			resp.Error = "get_state failed"
		}
		return nil, errors.New(resp.Error)
	}
	return resp.Data, nil
}

func (w *Wrapper) acceptLoop(ctx context.Context) error {
	for {
		conn, err := w.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			return err
		}
		p := &localPeer{conn: conn}
		id := w.registerPeer(p)
		go w.handleLocalPeer(ctx, id, p)
	}
}

func (w *Wrapper) handleLocalPeer(ctx context.Context, id int64, p *localPeer) {
	defer func() {
		_ = p.conn.Close()
		w.unregisterPeer(id)
	}()

	_ = protocol.ReadJSONL(p.conn, func(line []byte) error {
		env, err := protocol.DecodeEnvelope(line)
		if err != nil {
			_ = w.sendError(p, "", err)
			return nil
		}
		if env.Type != protocol.MessageCommand {
			_ = w.sendError(p, env.ID, fmt.Errorf("expected command envelope, got %s", env.Type))
			return nil
		}
		return w.handleCommand(ctx, p, env)
	})
}

func (w *Wrapper) handleCommand(ctx context.Context, p peer, env protocol.Envelope) error {
	var meta struct {
		Command string `json:"command"`
	}
	if err := env.DecodePayload(&meta); err != nil {
		_ = w.sendError(p, env.ID, err)
		return nil
	}

	switch meta.Command {
	case protocol.CommandInitialize:
		var init protocol.InitializeCommand
		if err := env.DecodePayload(&init); err != nil {
			_ = w.sendError(p, env.ID, err)
			return nil
		}
		p.setInitialized(true)
		capabilities := wrapperCapabilities()
		if err := w.sendAck(p, env.ID, protocol.CommandInitialize, map[string]any{
			"protocol_version": protocol.Version,
			"capabilities":     capabilities,
		}); err != nil {
			return err
		}
		ready := protocol.AgentReadyEvent{
			Event:           protocol.EventSessionAgentReady,
			ProtocolVersion: protocol.Version,
			Capabilities:    capabilities,
			Session:         protocol.SessionInfo{ID: w.cfg.SessionID},
			Runtime:         protocol.RuntimeInfo{ID: w.runtimeID, Kind: "pi", Transport: "rpc"},
		}
		return w.sendEvent(p, ready)

	case protocol.CommandSessionPrompt:
		if !p.initialized() {
			_ = w.sendError(p, env.ID, errors.New("initialize must be sent before session.prompt"))
			return nil
		}
		var prompt protocol.SessionPromptCommand
		if err := env.DecodePayload(&prompt); err != nil {
			_ = w.sendError(p, env.ID, err)
			return nil
		}
		piCmd, err := promptToPICommand(prompt)
		if err != nil {
			_ = w.sendError(p, env.ID, err)
			return nil
		}
		piCmd["id"] = "prompt-" + env.ID
		resp, err := w.sendPICommand(ctx, piCmd)
		if err != nil {
			_ = w.sendError(p, env.ID, err)
			return nil
		}
		if !resp.Success {
			_ = w.sendError(p, env.ID, errors.New(resp.Error))
			return nil
		}
		return w.sendAck(p, env.ID, protocol.CommandSessionPrompt, nil)

	case protocol.CommandSessionCancel:
		if !p.initialized() {
			_ = w.sendError(p, env.ID, errors.New("initialize must be sent before session.cancel"))
			return nil
		}
		resp, err := w.sendPICommand(ctx, map[string]any{"id": "cancel-" + env.ID, "type": "abort"})
		if err != nil {
			_ = w.sendError(p, env.ID, err)
			return nil
		}
		if !resp.Success {
			_ = w.sendError(p, env.ID, errors.New(resp.Error))
			return nil
		}
		return w.sendAck(p, env.ID, protocol.CommandSessionCancel, nil)
	default:
		_ = w.sendError(p, env.ID, fmt.Errorf("unknown command %q", meta.Command))
		return nil
	}
}

func promptToPICommand(prompt protocol.SessionPromptCommand) (map[string]any, error) {
	msg := strings.TrimSpace(prompt.Message)
	if msg == "" {
		return nil, errors.New("empty prompt")
	}
	switch prompt.Delivery {
	case "", "default", "foreground":
		return map[string]any{"type": "prompt", "message": msg}, nil
	case "interrupt":
		return map[string]any{"type": "steer", "message": msg}, nil
	case "follow_up", "queue":
		return map[string]any{"type": "follow_up", "message": msg}, nil
	default:
		return nil, fmt.Errorf("unsupported delivery %q", prompt.Delivery)
	}
}

func (w *Wrapper) sendPICommand(ctx context.Context, cmd map[string]any) (piResponse, error) {
	id, _ := cmd["id"].(string)
	if id == "" {
		id = fmt.Sprintf("cmd-%d", time.Now().UTC().UnixNano())
		cmd["id"] = id
	}
	ch := make(chan piResponse, 1)
	w.pendingMu.Lock()
	w.pending[id] = ch
	w.pendingMu.Unlock()
	defer func() {
		w.pendingMu.Lock()
		delete(w.pending, id)
		w.pendingMu.Unlock()
	}()

	w.piInMu.Lock()
	err := protocol.WriteJSONLine(w.piIn, cmd)
	w.piInMu.Unlock()
	if err != nil {
		return piResponse{}, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return piResponse{}, ctx.Err()
	case <-time.After(w.cfg.StartupTimeout):
		return piResponse{}, fmt.Errorf("timed out waiting for pi response to %s", id)
	}
}

func (w *Wrapper) readPiOutput(stdout io.Reader) {
	_ = protocol.ReadJSONL(stdout, func(line []byte) error {
		var msg map[string]any
		if err := json.Unmarshal(line, &msg); err != nil {
			w.broadcastUpdate(protocol.SessionUpdate{Kind: protocol.UpdateError, Message: "invalid pi rpc output", Details: map[string]any{"error": err.Error(), "line": string(line)}})
			return nil
		}
		typ := stringValue(msg["type"])
		if typ == "response" {
			resp := piResponse{
				ID:      stringValue(msg["id"]),
				Command: stringValue(msg["command"]),
				Success: boolValue(msg["success"]),
				Error:   stringValue(msg["error"]),
				Data:    mapValue(msg["data"]),
			}
			w.pendingMu.Lock()
			ch := w.pending[resp.ID]
			w.pendingMu.Unlock()
			if ch != nil {
				ch <- resp
			}
			return nil
		}
		for _, update := range normalizePIEvent(msg) {
			w.broadcastUpdate(update)
		}
		return nil
	})
}

func normalizePIEvent(msg map[string]any) []protocol.SessionUpdate {
	typ := stringValue(msg["type"])
	switch typ {
	case "agent_start":
		return []protocol.SessionUpdate{{Kind: protocol.UpdateLifecycle, Phase: "running", Details: msg}}
	case "agent_end":
		return []protocol.SessionUpdate{{Kind: protocol.UpdateComplete, Details: msg}}
	case "message_update":
		delta := mapValue(msg["assistantMessageEvent"])
		if stringValue(delta["type"]) == "text_delta" {
			return []protocol.SessionUpdate{{Kind: protocol.UpdateMessageDelta, Delta: stringValue(delta["delta"]), Details: msg}}
		}
		if stringValue(delta["type"]) == "error" {
			return []protocol.SessionUpdate{{Kind: protocol.UpdateError, Message: stringValue(delta["reason"]), Details: msg}}
		}
		return nil
	case "message_end":
		message := mapValue(msg["message"])
		if stringValue(message["role"]) != "assistant" {
			return nil
		}
		return []protocol.SessionUpdate{{Kind: protocol.UpdateMessageComplete, Message: extractAssistantText(message), Details: msg}}
	case "tool_execution_start":
		return []protocol.SessionUpdate{{Kind: protocol.UpdateToolBegin, ToolCallID: stringValue(msg["toolCallId"]), ToolName: stringValue(msg["toolName"]), Details: msg}}
	case "tool_execution_end":
		return []protocol.SessionUpdate{{Kind: protocol.UpdateToolEnd, ToolCallID: stringValue(msg["toolCallId"]), ToolName: stringValue(msg["toolName"]), IsError: boolValue(msg["isError"]), Details: msg}}
	case "extension_error":
		return []protocol.SessionUpdate{{Kind: protocol.UpdateError, Message: stringValue(msg["error"]), Details: msg}}
	default:
		return nil
	}
}

func extractAssistantText(message map[string]any) string {
	content := message["content"]
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			entry := mapValue(item)
			if stringValue(entry["type"]) == "text" {
				text := stringValue(entry["text"])
				if text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func (w *Wrapper) sendAck(p peer, id, command string, data map[string]any) error {
	env, err := protocol.NewEnvelope(protocol.MessageAck, w.cfg.SessionID, w.runtimeID, "weave-wrapper", id, protocol.AckPayload{
		Command: command,
		Success: true,
		Data:    data,
	})
	if err != nil {
		return err
	}
	return p.writeEnvelope(env)
}

func (w *Wrapper) sendError(p peer, id string, commandErr error) error {
	env, err := protocol.NewEnvelope(protocol.MessageError, w.cfg.SessionID, w.runtimeID, "weave-wrapper", id, protocol.ErrorPayload{Error: commandErr.Error()})
	if err != nil {
		return err
	}
	return p.writeEnvelope(env)
}

func (w *Wrapper) sendEvent(p peer, payload any) error {
	env, err := protocol.NewEnvelope(protocol.MessageEvent, w.cfg.SessionID, w.runtimeID, "weave-wrapper", "", payload)
	if err != nil {
		return err
	}
	return p.writeEnvelope(env)
}

func (w *Wrapper) broadcastUpdate(update protocol.SessionUpdate) {
	event := protocol.SessionUpdateEvent{Event: protocol.EventSessionUpdate, Update: update}
	env, err := protocol.NewEnvelope(protocol.MessageEvent, w.cfg.SessionID, w.runtimeID, "weave-wrapper", "", event)
	if err != nil {
		return
	}
	for _, p := range w.snapshotInitializedPeers() {
		_ = p.writeEnvelope(env)
	}
}

func (w *Wrapper) registerPeer(p peer) int64 {
	id := atomic.AddInt64(&w.nextID, 1)
	w.peersMu.Lock()
	w.peers[id] = p
	w.peersMu.Unlock()
	return id
}

func (w *Wrapper) unregisterPeer(id int64) {
	w.peersMu.Lock()
	delete(w.peers, id)
	w.peersMu.Unlock()
}

func (w *Wrapper) snapshotInitializedPeers() []peer {
	w.peersMu.Lock()
	defer w.peersMu.Unlock()
	peers := make([]peer, 0, len(w.peers))
	for _, p := range w.peers {
		if p.initialized() {
			peers = append(peers, p)
		}
	}
	return peers
}

func (w *Wrapper) logStderr(stderr io.Reader) {
	_, _ = io.Copy(logWriter{logger: w.cfg.Logger}, stderr)
}

func wrapperCapabilities() map[string]bool {
	return map[string]bool{
		"session_prompt":     true,
		"session_cancel":     true,
		"session_update":     true,
		"delivery_interrupt": true,
		"delivery_follow_up": true,
	}
}

func (p *localPeer) writeEnvelope(env protocol.Envelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return protocol.WriteJSONLine(p.conn, env)
}

func (p *localPeer) setInitialized(v bool) { p.init = v }
func (p *localPeer) initialized() bool     { return p.init }

func (p *relayPeer) writeEnvelope(env protocol.Envelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn.WriteJSON(env)
}

func (p *relayPeer) setInitialized(v bool) { p.init = v }
func (p *relayPeer) initialized() bool     { return p.init }

type logWriter struct{ logger *log.Logger }

func (w logWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text != "" {
		w.logger.Print(text)
	}
	return len(p), nil
}

func containsArg(args []string, name string) bool {
	for i := 0; i < len(args); i++ {
		if args[i] == name || strings.HasPrefix(args[i], name+"=") {
			return true
		}
	}
	return false
}

func containsAny(args []string, values ...string) bool {
	for _, value := range values {
		if containsArg(args, value) {
			return true
		}
	}
	return false
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
