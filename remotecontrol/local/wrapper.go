package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
	"github.com/victorarias/agentic-weave/remotecontrol/runtime"
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

	runtimeID  string
	descriptor runtime.Descriptor

	listener *net.UnixListener

	peersMu sync.Mutex
	peers   map[int64]peer
	nextID  int64

	piInMu sync.Mutex
	piIn   io.WriteCloser

	pendingMu sync.Mutex
	pending   map[string]chan piResponse

	bootstrapState map[string]any

	permissionMu      sync.Mutex
	pendingPermission map[string]map[string]any

	closed chan struct{}
}

func NewWrapper(cfg Config) *Wrapper {
	descriptor := runtime.PiRPC()
	if cfg.SocketPath == "" {
		cfg.SocketPath = filepath.Join(os.TempDir(), "weave-local.sock")
	}
	if cfg.SessionID == "" {
		cfg.SessionID = "local"
	}
	if cfg.PiBin == "" {
		cfg.PiBin = descriptor.Command
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = descriptor.StartupTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	return &Wrapper{
		cfg:               cfg,
		runtimeID:         fmt.Sprintf("rt-%d", time.Now().UTC().UnixNano()),
		descriptor:        descriptor,
		peers:             make(map[int64]peer),
		pending:           make(map[string]chan piResponse),
		pendingPermission: make(map[string]map[string]any),
		closed:            make(chan struct{}),
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
		capabilities := w.descriptor.Capabilities
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
			Runtime:         protocol.RuntimeInfo{ID: w.runtimeID, Kind: "pi", Transport: w.descriptor.Transport},
		}
		return w.sendEvent(p, ready)

	case protocol.CommandSessionStatus:
		if !p.initialized() {
			_ = w.sendError(p, env.ID, errors.New("initialize must be sent before session.status"))
			return nil
		}
		return w.sendAck(p, env.ID, protocol.CommandSessionStatus, map[string]any{
			"session":                  protocol.SessionInfo{ID: w.cfg.SessionID},
			"runtime":                  protocol.RuntimeInfo{ID: w.runtimeID, Kind: "pi", Transport: w.descriptor.Transport},
			"persisted_session_handle": stringValue(w.bootstrapState["sessionFile"]),
			"runtime_descriptor": map[string]any{
				"name":                w.descriptor.Name,
				"command":             w.descriptor.Command,
				"args":                w.descriptor.Args,
				"transport":           w.descriptor.Transport,
				"resume_strategy":     w.descriptor.ResumeStrategy,
				"supports_permission": w.descriptor.SupportsPermission,
			},
		})
	case protocol.CommandSessionPermissionResponse:
		if !p.initialized() {
			_ = w.sendError(p, env.ID, errors.New("initialize must be sent before session.permission_response"))
			return nil
		}
		var resp protocol.PermissionResponseCommand
		if err := env.DecodePayload(&resp); err != nil {
			_ = w.sendError(p, env.ID, err)
			return nil
		}
		if err := w.respondToPermission(resp); err != nil {
			_ = w.sendError(p, env.ID, err)
			return nil
		}
		if err := w.sendAck(p, env.ID, protocol.CommandSessionPermissionResponse, nil); err != nil {
			return err
		}
		w.broadcastUpdate(protocol.SessionUpdate{Kind: protocol.UpdatePermissionResolved, RequestID: resp.RequestID, Decision: resp.Decision, Details: map[string]any{"request_id": resp.RequestID, "decision": resp.Decision}})
		w.broadcastUpdate(protocol.SessionUpdate{Kind: protocol.UpdateStatus, Phase: "running", Details: map[string]any{"run_state": "running"}})
		return nil
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
	case "interrupt", "steer":
		return map[string]any{"type": "steer", "message": msg}, nil
	case "follow_up", "queue", "deliver_when_idle":
		return map[string]any{"type": "follow_up", "message": msg}, nil
	default:
		return nil, fmt.Errorf("unsupported delivery %q", prompt.Delivery)
	}
}

func (w *Wrapper) respondToPermission(resp protocol.PermissionResponseCommand) error {
	w.permissionMu.Lock()
	request, ok := w.pendingPermission[resp.RequestID]
	if ok {
		delete(w.pendingPermission, resp.RequestID)
	}
	w.permissionMu.Unlock()
	if !ok {
		return fmt.Errorf("unknown permission request %q", resp.RequestID)
	}
	method := stringValue(request["method"])
	payload := map[string]any{"type": "extension_ui_response", "id": resp.RequestID}
	switch method {
	case "confirm":
		payload["confirmed"] = resp.Decision == "allow"
		if resp.Decision != "allow" && resp.Decision != "deny" {
			return fmt.Errorf("unsupported permission decision %q", resp.Decision)
		}
		if resp.Decision == "deny" {
			payload["confirmed"] = false
		}
		w.piInMu.Lock()
		err := protocol.WriteJSONLine(w.piIn, payload)
		w.piInMu.Unlock()
		return err
	default:
		return fmt.Errorf("unsupported permission request method %q", method)
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

func (w *Wrapper) nextPeerID() int64 {
	return atomic.AddInt64(&w.nextID, 1)
}

func (w *Wrapper) logStderr(stderr io.Reader) {
	_, _ = io.Copy(logWriter{logger: w.cfg.Logger}, stderr)
}

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
