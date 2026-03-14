package relay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
	"github.com/victorarias/agentic-weave/remotecontrol/runtime"
	"github.com/victorarias/agentic-weave/remotecontrol/session"
)

type Config struct {
	Addr           string
	Token          string
	Logger         *log.Logger
	SessionDir     string
	PublicURL      string
	WrapperBin     string
	PiBin          string
	PTYBin         string
	StartupTimeout time.Duration
	Launcher       Launcher
}

type Server struct {
	cfg      Config
	upgrader websocket.Upgrader

	mu             sync.Mutex
	wrappers       map[string]*connState
	attachments    map[string]*connState
	pendingPrompts map[string][]protocol.Envelope
	subscribers    map[string]map[*connState]struct{}
	registry       *session.Registry
	launcher       Launcher
}

type connState struct {
	conn         *websocket.Conn
	role         string
	identity     string
	sessionID    string
	runtimeID    string
	attachedTo   string
	attachedMode string
	mu           sync.Mutex
	authed       bool
	sessions     map[string]struct{}
}

const relayWriteTimeout = time.Second

func NewServer(cfg Config) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(nilWriter{}, "", 0)
	}
	if cfg.SessionDir == "" {
		cfg.SessionDir = filepath.Join(os.TempDir(), "weave-relay-sessions")
	}
	if cfg.PiBin == "" {
		cfg.PiBin = runtime.PiRPC().Command
	}
	if cfg.PTYBin == "" {
		cfg.PTYBin = runtime.PiPTY().Command
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = 20 * time.Second
	}
	launcher := cfg.Launcher
	if launcher == nil {
		launcher = NewProcessLauncher(cfg.WrapperBin, cfg.Logger)
	}
	return &Server{
		cfg: cfg,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		wrappers:       make(map[string]*connState),
		attachments:    make(map[string]*connState),
		pendingPrompts: make(map[string][]protocol.Envelope),
		subscribers:    make(map[string]map[*connState]struct{}),
		registry:       session.NewRegistry(),
		launcher:       launcher,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/", s.handleWS)
	return mux
}

func (s *Server) Run(ctx context.Context) error {
	httpServer := &http.Server{Addr: s.cfg.Addr, Handler: s.Handler()}
	errCh := make(chan error, 1)
	go func() {
		err := httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		_ = s.launcher.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	state := &connState{conn: conn, sessions: make(map[string]struct{})}
	defer func() {
		s.unregister(state)
		_ = conn.Close()
	}()

	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			return
		}
		if !state.authed {
			s.handleAuth(state, env)
			continue
		}
		switch state.role {
		case protocol.RoleClient:
			s.handleClientEnvelope(state, env)
		case protocol.RoleWrapper:
			s.handleWrapperEnvelope(state, env)
		default:
			_ = s.sendError(state, env.ID, env.SessionID, "unknown connection role")
		}
	}
}

func (s *Server) handleAuth(state *connState, env protocol.Envelope) {
	if env.Type != protocol.MessageCommand {
		_ = s.sendError(state, env.ID, env.SessionID, "auth required before other message types")
		return
	}
	var cmd protocol.AuthCommand
	if err := env.DecodePayload(&cmd); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if cmd.Command != protocol.CommandAuth {
		_ = s.sendError(state, env.ID, env.SessionID, "first command must be auth")
		return
	}
	if s.cfg.Token != "" && cmd.Token != s.cfg.Token {
		_ = s.sendError(state, env.ID, env.SessionID, "invalid token")
		return
	}
	if cmd.Role != protocol.RoleClient && cmd.Role != protocol.RoleWrapper {
		_ = s.sendError(state, env.ID, env.SessionID, "invalid auth role")
		return
	}
	transport := strings.TrimSpace(cmd.Transport)
	if cmd.Role == protocol.RoleWrapper {
		if transport == "" {
			transport = runtime.PiRPC().Transport
		}
		if transport != runtime.PiRPC().Transport && transport != runtime.PiPTY().Transport {
			_ = s.sendError(state, env.ID, env.SessionID, fmt.Sprintf("invalid wrapper transport %q", cmd.Transport))
			return
		}
	}
	if cmd.Role == protocol.RoleWrapper && env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "wrapper auth requires session_id")
		return
	}
	if cmd.Role == protocol.RoleWrapper && env.RuntimeID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "wrapper auth requires runtime_id")
		return
	}

	identity := env.From
	if identity == "" {
		identity = cmd.Role
	}

	s.mu.Lock()
	if cmd.Role == protocol.RoleWrapper {
		if existing := s.wrappers[env.SessionID]; existing != nil {
			s.mu.Unlock()
			_ = s.sendError(state, env.ID, env.SessionID, "wrapper already registered for session")
			return
		}
		s.wrappers[env.SessionID] = state
		s.registry.SetConnected(env.SessionID, protocol.RuntimeInfo{ID: env.RuntimeID, Kind: "pi", Transport: transport}, "")
	}
	s.mu.Unlock()

	state.authed = true
	state.role = cmd.Role
	state.identity = identity
	state.sessionID = env.SessionID
	state.runtimeID = env.RuntimeID

	ack, err := protocol.NewEnvelope(protocol.MessageAck, env.SessionID, "", "weave-relay", env.ID, protocol.AckPayload{
		Command: protocol.CommandAuth,
		Success: true,
		Data: map[string]any{
			"role": cmd.Role,
		},
	})
	if err != nil {
		return
	}
	_ = state.writeEnvelope(ack)
}

func (s *Server) handleClientEnvelope(state *connState, env protocol.Envelope) {
	if env.Type != protocol.MessageCommand {
		_ = s.sendError(state, env.ID, env.SessionID, "clients may only send command envelopes")
		return
	}
	if env.SessionID == "" {
		var meta struct {
			Command string `json:"command"`
		}
		if err := env.DecodePayload(&meta); err != nil || (meta.Command != protocol.CommandAuth && meta.Command != protocol.CommandRegistryListSessions) {
			_ = s.sendError(state, env.ID, env.SessionID, "session_id is required")
			return
		}
	}

	var meta struct {
		Command string `json:"command"`
	}
	if err := env.DecodePayload(&meta); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	switch meta.Command {
	case protocol.CommandRegistryListSessions:
		s.handleListSessions(state, env)
		return
	case protocol.CommandSessionStatus:
		s.handleSessionStatus(state, env)
		return
	case protocol.CommandSessionSpawn:
		s.handleSessionSpawn(state, env)
		return
	case protocol.CommandSessionLoad:
		s.handleSessionLoad(state, env)
		return
	case protocol.CommandRuntimeStop:
		s.handleRuntimeStop(state, env)
		return
	case protocol.CommandRuntimeReplace:
		s.handleRuntimeReplace(state, env)
		return
	case protocol.CommandSessionPermissionResponse:
		s.handlePermissionResponse(state, env)
		return
	case protocol.CommandSessionAttach:
		s.handleSessionAttach(state, env)
		return
	case protocol.CommandSessionDetach:
		s.handleSessionDetach(state, env)
		return
	case protocol.CommandPTYInput:
		s.handlePTYInput(state, env)
		return
	case protocol.CommandPTYResize:
		s.handlePTYResize(state, env)
		return
	}

	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required")
		return
	}
	if meta.Command == protocol.CommandInitialize || meta.Command == protocol.CommandSessionPrompt || meta.Command == protocol.CommandSessionCancel {
		s.subscribe(state, env.SessionID)
	}
	if meta.Command == protocol.CommandSessionPrompt {
		if queued, err := s.tryQueuePrompt(state, env); err != nil {
			_ = s.sendError(state, env.ID, env.SessionID, err.Error())
			return
		} else if queued {
			return
		}
		if !s.canPrompt(state, env.SessionID) {
			_ = s.sendError(state, env.ID, env.SessionID, "attached client in observe mode cannot send prompts")
			return
		}
	}

	s.mu.Lock()
	wrapper := s.wrappers[env.SessionID]
	s.mu.Unlock()
	if wrapper == nil {
		_ = s.sendError(state, env.ID, env.SessionID, "no wrapper connected for session")
		return
	}
	_ = wrapper.writeEnvelope(env)
}

func (s *Server) handleWrapperEnvelope(state *connState, env protocol.Envelope) {
	sessionID := env.SessionID
	if sessionID == "" {
		sessionID = state.sessionID
		env.SessionID = sessionID
	}
	if sessionID != state.sessionID {
		_ = s.sendError(state, env.ID, sessionID, "wrapper may only send messages for its authenticated session")
		return
	}

	s.mu.Lock()
	if s.wrappers[sessionID] != state {
		s.mu.Unlock()
		_ = s.sendError(state, env.ID, sessionID, "wrapper is not registered for session")
		return
	}
	subs := s.subscribers[sessionID]
	clients := make([]*connState, 0, len(subs))
	for client := range subs {
		clients = append(clients, client)
	}
	s.mu.Unlock()

	if isPTYOutputEnvelope(env) {
		s.forwardPTYOutput(sessionID, env)
		return
	}
	s.applyWrapperEnvelope(state, env)
	for _, client := range clients {
		_ = client.writeEnvelope(env)
	}
}

func (s *Server) handleListSessions(state *connState, env protocol.Envelope) {
	records := s.registry.List()
	sessions := make([]map[string]any, 0, len(records))
	for _, record := range records {
		sessions = append(sessions, map[string]any{
			"session":                  record.Session,
			"runtime":                  record.Runtime,
			"persisted_session_handle": record.PersistedSessionHandle,
			"wrapper_connected":        record.WrapperConnected,
			"state":                    record.State,
			"phase":                    record.Phase,
			"attachment":               record.Attachment,
			"queued_prompts":           record.QueuedPrompts,
			"pty_rows":                 record.PTYRows,
			"pty_cols":                 record.PTYCols,
			"pending_permissions":      record.PendingPermissions,
			"updated_at":               record.UpdatedAt.Format(time.RFC3339Nano),
		})
	}
	ack, err := protocol.NewEnvelope(protocol.MessageAck, "", "", "weave-relay", env.ID, protocol.AckPayload{
		Command: protocol.CommandRegistryListSessions,
		Success: true,
		Data: map[string]any{
			"sessions": sessions,
		},
	})
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	_ = state.writeEnvelope(ack)
}

func (s *Server) handleSessionStatus(state *connState, env protocol.Envelope) {
	record, ok := s.registry.Get(env.SessionID)
	if !ok {
		_ = s.sendError(state, env.ID, env.SessionID, "unknown session")
		return
	}
	_ = state.writeEnvelope(mustAckEnvelope(env.ID, env.SessionID, record, protocol.CommandSessionStatus))
}

func (s *Server) handleSessionSpawn(state *connState, env protocol.Envelope) {
	var cmd protocol.SessionSpawnCommand
	if err := env.DecodePayload(&cmd); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	descriptor, err := runtimeDescriptorForTransport(cmd.Transport)
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for session.spawn")
		return
	}
	if record, ok := s.registry.Get(env.SessionID); ok && record.WrapperConnected {
		_ = s.sendError(state, env.ID, env.SessionID, "session already has a connected runtime")
		return
	}
	persistedHandle, err := resolveSessionHandle(s.cfg.SessionDir, env.SessionID, cmd.SessionPath)
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(persistedHandle), 0o755); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	s.registry.Ensure(env.SessionID, persistedHandle)
	if err := s.launcher.Spawn(context.Background(), LaunchRequest{
		SessionID:              env.SessionID,
		PersistedSessionHandle: persistedHandle,
		RelayURL:               s.relayURL(),
		Token:                  s.cfg.Token,
		PiBin:                  s.cfg.PiBin,
		PTYBin:                 s.cfg.PTYBin,
		RuntimeDescriptor:      descriptor,
	}); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, normalizeSpawnError(err))
		return
	}
	record, err := s.waitForConnected(env.SessionID)
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	_ = state.writeEnvelope(mustAckEnvelope(env.ID, env.SessionID, record, protocol.CommandSessionSpawn))
}

func (s *Server) handleSessionLoad(state *connState, env protocol.Envelope) {
	var cmd protocol.SessionLoadCommand
	if err := env.DecodePayload(&cmd); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	descriptor, err := runtimeDescriptorForTransport(cmd.Transport)
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for session.load")
		return
	}
	if record, ok := s.registry.Get(env.SessionID); ok && record.WrapperConnected {
		_ = s.sendError(state, env.ID, env.SessionID, "session already has a connected runtime")
		return
	}
	record, _ := s.registry.Get(env.SessionID)
	requestedHandle := cmd.SessionPath
	if requestedHandle == "" {
		requestedHandle = record.PersistedSessionHandle
	}
	if requestedHandle == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session.load requires a known persisted session handle")
		return
	}
	persistedHandle, err := resolveSessionHandle(s.cfg.SessionDir, env.SessionID, requestedHandle)
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if _, err := os.Stat(persistedHandle); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, fmt.Sprintf("persisted session handle not found: %s", persistedHandle))
		return
	}
	s.registry.Ensure(env.SessionID, persistedHandle)
	if err := s.launcher.Spawn(context.Background(), LaunchRequest{
		SessionID:              env.SessionID,
		PersistedSessionHandle: persistedHandle,
		RelayURL:               s.relayURL(),
		Token:                  s.cfg.Token,
		PiBin:                  s.cfg.PiBin,
		PTYBin:                 s.cfg.PTYBin,
		RuntimeDescriptor:      descriptor,
	}); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, normalizeSpawnError(err))
		return
	}
	record, err = s.waitForConnected(env.SessionID)
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	_ = state.writeEnvelope(mustAckEnvelope(env.ID, env.SessionID, record, protocol.CommandSessionLoad))
}

func (s *Server) handleRuntimeStop(state *connState, env protocol.Envelope) {
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for runtime.stop")
		return
	}
	if err := s.launcher.Stop(env.SessionID); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, fmt.Sprintf("failed to stop runtime: %v", err))
		return
	}
	record, err := s.waitForDisconnected(env.SessionID)
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	_ = state.writeEnvelope(mustAckEnvelope(env.ID, env.SessionID, record, protocol.CommandRuntimeStop))
}

func (s *Server) handleRuntimeReplace(state *connState, env protocol.Envelope) {
	var cmd protocol.RuntimeReplaceCommand
	if err := env.DecodePayload(&cmd); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for runtime.replace")
		return
	}
	record, err := s.replaceRuntimeTransport(env.SessionID, cmd.Transport)
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	_ = state.writeEnvelope(mustAckEnvelope(env.ID, env.SessionID, record, protocol.CommandRuntimeReplace))
}

func (s *Server) handleSessionAttach(state *connState, env protocol.Envelope) {
	var cmd protocol.SessionAttachCommand
	if err := env.DecodePayload(&cmd); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for session.attach")
		return
	}
	if cmd.Mode != "observe" && cmd.Mode != "inject" && cmd.Mode != "takeover" {
		_ = s.sendError(state, env.ID, env.SessionID, fmt.Sprintf("unsupported attach mode %q", cmd.Mode))
		return
	}
	if state.identity == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "attach requires client identity")
		return
	}
	record, ok := s.registry.Get(env.SessionID)
	if !ok {
		_ = s.sendError(state, env.ID, env.SessionID, "unknown session")
		return
	}
	existingReturnTransport := ""
	if record.Attachment != nil {
		existingReturnTransport = record.Attachment.ReturnTransport
	}
	returnTransport := ""
	if cmd.Mode == "takeover" {
		if !record.WrapperConnected {
			_ = s.sendError(state, env.ID, env.SessionID, "cannot take over stopped session; load or spawn first")
			return
		}
		var err error
		record, returnTransport, err = s.ensureTakeoverRuntime(env.SessionID, record)
		if err != nil {
			_ = s.sendError(state, env.ID, env.SessionID, err.Error())
			return
		}
	}

	var previousSessionID string
	previousSessionMode := ""
	previousReturnTransport := ""
	previousMode := ""
	action := "attached"
	s.mu.Lock()
	attachedSessionID, attachedOwner := s.findAttachmentByIdentityLocked(state.identity)
	if attachedSessionID != "" && attachedSessionID != env.SessionID {
		previousSessionID = attachedSessionID
		if attachedOwner != nil {
			previousSessionMode = attachedOwner.attachedMode
		}
		if previousRecord, ok := s.registry.Get(attachedSessionID); ok && previousRecord.Attachment != nil {
			previousReturnTransport = previousRecord.Attachment.ReturnTransport
		}
		delete(s.attachments, attachedSessionID)
		if attachedOwner != nil {
			attachedOwner.attachedTo = ""
			attachedOwner.attachedMode = ""
		}
	}
	attached := s.attachments[env.SessionID]
	if attached != nil && !sameAttachmentOwner(attached, state) {
		s.mu.Unlock()
		_ = s.sendError(state, env.ID, env.SessionID, "another controller is already attached")
		return
	}
	if attached != nil {
		previousMode = attached.attachedMode
		if previousMode != "" && previousMode != cmd.Mode {
			action = "mode_changed"
		}
		if attached != state {
			attached.attachedTo = ""
			attached.attachedMode = ""
		}
	}
	s.attachments[env.SessionID] = state
	state.attachedTo = env.SessionID
	state.attachedMode = cmd.Mode
	s.mu.Unlock()

	if previousSessionID != "" {
		s.registry.ClearAttachment(previousSessionID)
	}
	s.subscribe(state, env.SessionID)
	record, _ = s.registry.SetAttachment(env.SessionID, protocol.AttachmentInfo{ClientID: state.identity, Mode: cmd.Mode, ReturnTransport: returnTransport})
	_ = state.writeEnvelope(mustAckEnvelope(env.ID, env.SessionID, record, protocol.CommandSessionAttach))
	if previousSessionID != "" {
		s.broadcastSessionStatus(previousSessionID, map[string]any{"attachment_action": "detached", "attachment_client_id": state.identity})
		if previousSessionMode == "takeover" {
			if _, err := s.restorePostTakeoverTransport(previousSessionID, previousReturnTransport); err == nil {
				s.flushQueuedPrompts(previousSessionID)
			}
		}
	}
	details := map[string]any{"attachment_action": action}
	if previousMode != "" {
		details["previous_mode"] = previousMode
	}
	s.broadcastSessionStatus(env.SessionID, details)
	if previousMode == "takeover" && cmd.Mode != "takeover" {
		if _, err := s.restorePostTakeoverTransport(env.SessionID, existingReturnTransport); err == nil {
			s.flushQueuedPrompts(env.SessionID)
		}
	}
}

func (s *Server) handleSessionDetach(state *connState, env protocol.Envelope) {
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for session.detach")
		return
	}
	cleared, previousAttachment, record, err := s.detachIfOwner(state, env.SessionID)
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if !cleared {
		_ = s.sendError(state, env.ID, env.SessionID, "client is not attached to session")
		return
	}
	if previousAttachment != nil && previousAttachment.Mode == "takeover" {
		if restored, restoreErr := s.restorePostTakeoverTransport(env.SessionID, previousAttachment.ReturnTransport); restoreErr == nil {
			record = restored
			defer s.flushQueuedPrompts(env.SessionID)
		}
	}
	_ = state.writeEnvelope(mustAckEnvelope(env.ID, env.SessionID, record, protocol.CommandSessionDetach))
	s.broadcastSessionStatus(env.SessionID, map[string]any{"attachment_action": "detached", "attachment_client_id": state.identity})
}

func (s *Server) handlePermissionResponse(state *connState, env protocol.Envelope) {
	var cmd protocol.PermissionResponseCommand
	if err := env.DecodePayload(&cmd); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for session.permission_response")
		return
	}
	record, ok := s.registry.Get(env.SessionID)
	if !ok {
		_ = s.sendError(state, env.ID, env.SessionID, "unknown session")
		return
	}
	if !s.registry.HasPendingPermission(env.SessionID, cmd.RequestID) {
		_ = s.sendError(state, env.ID, env.SessionID, fmt.Sprintf("unknown permission request %q", cmd.RequestID))
		return
	}
	if !s.canRespondToPermission(state, record) {
		_ = s.sendError(state, env.ID, env.SessionID, "permission authority is held by attached human")
		return
	}
	if !record.WrapperConnected {
		_ = s.sendError(state, env.ID, env.SessionID, "no wrapper connected for session")
		return
	}
	s.mu.Lock()
	wrapper := s.wrappers[env.SessionID]
	s.mu.Unlock()
	if wrapper == nil {
		_ = s.sendError(state, env.ID, env.SessionID, "no wrapper connected for session")
		return
	}
	_ = wrapper.writeEnvelope(env)
}

func (s *Server) handlePTYInput(state *connState, env protocol.Envelope) {
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for pty.input")
		return
	}
	record, ok := s.registry.Get(env.SessionID)
	if !ok {
		_ = s.sendError(state, env.ID, env.SessionID, "unknown session")
		return
	}
	if !record.WrapperConnected {
		_ = s.sendError(state, env.ID, env.SessionID, "no wrapper connected for session")
		return
	}
	if !s.canUsePTY(state, record) {
		_ = s.sendError(state, env.ID, env.SessionID, "pty authority is held by attached human in takeover")
		return
	}
	s.mu.Lock()
	wrapper := s.wrappers[env.SessionID]
	s.mu.Unlock()
	if wrapper == nil {
		_ = s.sendError(state, env.ID, env.SessionID, "no wrapper connected for session")
		return
	}
	_ = wrapper.writeEnvelope(env)
}

func (s *Server) handlePTYResize(state *connState, env protocol.Envelope) {
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for pty.resize")
		return
	}
	record, ok := s.registry.Get(env.SessionID)
	if !ok {
		_ = s.sendError(state, env.ID, env.SessionID, "unknown session")
		return
	}
	if !record.WrapperConnected {
		_ = s.sendError(state, env.ID, env.SessionID, "no wrapper connected for session")
		return
	}
	if !s.canUsePTY(state, record) {
		_ = s.sendError(state, env.ID, env.SessionID, "pty authority is held by attached human in takeover")
		return
	}
	s.mu.Lock()
	wrapper := s.wrappers[env.SessionID]
	s.mu.Unlock()
	if wrapper == nil {
		_ = s.sendError(state, env.ID, env.SessionID, "no wrapper connected for session")
		return
	}
	_ = wrapper.writeEnvelope(env)
}

func sameAttachmentOwner(owner, candidate *connState) bool {
	if owner == nil || candidate == nil {
		return false
	}
	if owner == candidate {
		return true
	}
	return owner.role == candidate.role && owner.identity != "" && owner.identity == candidate.identity
}

func (s *Server) findAttachmentByIdentityLocked(identity string) (string, *connState) {
	if identity == "" {
		return "", nil
	}
	for sessionID, owner := range s.attachments {
		if owner != nil && owner.identity == identity {
			return sessionID, owner
		}
	}
	return "", nil
}

func isPTYOutputEnvelope(env protocol.Envelope) bool {
	if env.Type != protocol.MessageEvent {
		return false
	}
	var meta struct {
		Event string `json:"event"`
	}
	if err := env.DecodePayload(&meta); err != nil {
		return false
	}
	return meta.Event == protocol.EventPTYOutput
}

func (s *Server) forwardPTYOutput(sessionID string, env protocol.Envelope) {
	s.mu.Lock()
	owner := s.attachments[sessionID]
	s.mu.Unlock()
	if owner == nil || owner.attachedMode != "takeover" {
		return
	}
	_ = owner.writeEnvelope(env)
}

func (s *Server) tryQueuePrompt(state *connState, env protocol.Envelope) (bool, error) {
	record, ok := s.registry.Get(env.SessionID)
	if !ok || record.Attachment == nil || record.Attachment.Mode != "takeover" {
		return false, nil
	}
	if record.Attachment.ClientID == state.identity {
		return false, nil
	}

	s.mu.Lock()
	s.pendingPrompts[env.SessionID] = append(s.pendingPrompts[env.SessionID], env)
	queued := len(s.pendingPrompts[env.SessionID])
	s.mu.Unlock()
	record, _ = s.registry.SetQueuedPrompts(env.SessionID, queued)
	if err := state.writeEnvelope(mustAckEnvelopeWithData(env.ID, env.SessionID, protocol.CommandSessionPrompt, map[string]any{
		"queued":         true,
		"queued_prompts": queued,
		"reason":         "takeover",
	})); err != nil {
		return true, err
	}
	s.broadcastSessionStatus(env.SessionID, map[string]any{"takeover_queue_action": "enqueued", "queued_prompts": record.QueuedPrompts})
	return true, nil
}

func (s *Server) flushQueuedPrompts(sessionID string) {
	s.mu.Lock()
	queue := append([]protocol.Envelope(nil), s.pendingPrompts[sessionID]...)
	wrapper := s.wrappers[sessionID]
	if wrapper != nil {
		delete(s.pendingPrompts, sessionID)
	}
	s.mu.Unlock()
	if wrapper == nil || len(queue) == 0 {
		return
	}
	s.registry.SetQueuedPrompts(sessionID, 0)
	for _, env := range queue {
		_ = wrapper.writeEnvelope(env)
	}
	s.broadcastSessionStatus(sessionID, map[string]any{"takeover_queue_action": "flushed", "queued_prompts": 0})
}

func (s *Server) canPrompt(state *connState, sessionID string) bool {
	record, ok := s.registry.Get(sessionID)
	if !ok || record.Attachment == nil {
		return true
	}
	if record.Attachment.ClientID != state.identity {
		return true
	}
	return record.Attachment.Mode != "observe"
}

func (s *Server) canRespondToPermission(state *connState, record session.Record) bool {
	if record.Attachment == nil {
		return true
	}
	if record.Attachment.Mode != "inject" && record.Attachment.Mode != "takeover" {
		return true
	}
	return record.Attachment.ClientID == state.identity
}

func (s *Server) canUsePTY(state *connState, record session.Record) bool {
	if record.Attachment == nil {
		return false
	}
	if record.Attachment.Mode != "takeover" {
		return false
	}
	return record.Attachment.ClientID == state.identity
}

func (s *Server) detachIfOwner(state *connState, sessionID string) (bool, *protocol.AttachmentInfo, session.Record, error) {
	s.mu.Lock()
	owner := s.attachments[sessionID]
	if !sameAttachmentOwner(owner, state) {
		s.mu.Unlock()
		return false, nil, session.Record{}, nil
	}
	var previousAttachment *protocol.AttachmentInfo
	if record, ok := s.registry.Get(sessionID); ok && record.Attachment != nil {
		previousAttachment = &protocol.AttachmentInfo{ClientID: record.Attachment.ClientID, Mode: record.Attachment.Mode, ReturnTransport: record.Attachment.ReturnTransport}
	}
	delete(s.attachments, sessionID)
	if owner != nil {
		owner.attachedTo = ""
		owner.attachedMode = ""
	}
	state.attachedTo = ""
	state.attachedMode = ""
	s.mu.Unlock()
	record, ok := s.registry.ClearAttachment(sessionID)
	if !ok {
		return false, previousAttachment, session.Record{}, fmt.Errorf("unknown session")
	}
	return true, previousAttachment, record, nil
}

func (s *Server) broadcastSessionStatus(sessionID string, extraDetails map[string]any) {
	record, ok := s.registry.Get(sessionID)
	if !ok {
		return
	}
	phase := record.Phase
	if phase == "" {
		phase = record.State
	}
	details := map[string]any{"state": record.State, "queued_prompts": record.QueuedPrompts, "pty_rows": record.PTYRows, "pty_cols": record.PTYCols}
	for key, value := range extraDetails {
		details[key] = value
	}
	update := protocol.SessionUpdate{Kind: protocol.UpdateStatus, Phase: phase, Details: details}
	if record.Attachment != nil {
		update.Details["attachment"] = record.Attachment
	}
	if len(record.PendingPermissions) > 0 {
		update.Details["pending_permissions"] = record.PendingPermissions
	}
	event := protocol.SessionUpdateEvent{Event: protocol.EventSessionUpdate, Update: update}
	env, err := protocol.NewEnvelope(protocol.MessageEvent, sessionID, record.Runtime.ID, "weave-relay", "", event)
	if err != nil {
		return
	}
	s.mu.Lock()
	subs := s.subscribers[sessionID]
	clients := make([]*connState, 0, len(subs))
	for client := range subs {
		clients = append(clients, client)
	}
	s.mu.Unlock()
	for _, client := range clients {
		_ = client.writeEnvelope(env)
	}
}

func (s *Server) applyWrapperEnvelope(state *connState, env protocol.Envelope) {
	if env.Type != protocol.MessageEvent {
		return
	}
	var meta struct {
		Event string `json:"event"`
	}
	if err := env.DecodePayload(&meta); err != nil {
		return
	}
	if meta.Event == protocol.EventSessionAgentReady {
		var ready protocol.AgentReadyEvent
		if err := env.DecodePayload(&ready); err == nil {
			record, ok := s.registry.Get(env.SessionID)
			if !ok || !record.WrapperConnected || record.Runtime.ID != ready.Runtime.ID {
				s.registry.SetConnected(env.SessionID, ready.Runtime, "")
			}
		}
		return
	}
	if meta.Event != protocol.EventSessionUpdate {
		return
	}
	var evt protocol.SessionUpdateEvent
	if err := env.DecodePayload(&evt); err != nil {
		return
	}
	switch evt.Update.Kind {
	case protocol.UpdatePermissionRequest:
		if evt.Update.Permission != nil {
			s.registry.AddPermission(env.SessionID, state.runtimeID, *evt.Update.Permission)
		}
	case protocol.UpdatePermissionResolved:
		s.registry.ResolvePermission(env.SessionID, state.runtimeID, evt.Update.RequestID)
	case protocol.UpdateStatus:
		s.registry.SetPhase(env.SessionID, state.runtimeID, evt.Update.Phase)
		rows, hasRows := intValue(evt.Update.Details["pty_rows"])
		cols, hasCols := intValue(evt.Update.Details["pty_cols"])
		if hasRows || hasCols {
			s.registry.SetPTYSize(env.SessionID, rows, cols)
		}
	}
}

func (s *Server) subscribe(state *connState, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.sessions[sessionID] = struct{}{}
	if s.subscribers[sessionID] == nil {
		s.subscribers[sessionID] = make(map[*connState]struct{})
	}
	s.subscribers[sessionID][state] = struct{}{}
}

func (s *Server) unregister(state *connState) {
	var broadcastSessionID string
	var detachedAttachment *protocol.AttachmentInfo
	s.mu.Lock()
	if state.role == protocol.RoleWrapper && state.sessionID != "" {
		if s.wrappers[state.sessionID] == state {
			delete(s.wrappers, state.sessionID)
			delete(s.pendingPrompts, state.sessionID)
			s.registry.SetDisconnected(state.sessionID, state.runtimeID)
			if owner := s.attachments[state.sessionID]; owner != nil {
				delete(s.attachments, state.sessionID)
				owner.attachedTo = ""
				owner.attachedMode = ""
				broadcastSessionID = state.sessionID
			}
		}
	}
	if state.attachedTo != "" {
		if s.attachments[state.attachedTo] == state {
			if record, ok := s.registry.Get(state.attachedTo); ok && record.Attachment != nil {
				detachedAttachment = &protocol.AttachmentInfo{ClientID: record.Attachment.ClientID, Mode: record.Attachment.Mode, ReturnTransport: record.Attachment.ReturnTransport}
			}
			delete(s.attachments, state.attachedTo)
			s.registry.ClearAttachment(state.attachedTo)
			broadcastSessionID = state.attachedTo
		}
	}
	for sessionID := range state.sessions {
		subs := s.subscribers[sessionID]
		delete(subs, state)
		if len(subs) == 0 {
			delete(s.subscribers, sessionID)
		}
	}
	s.mu.Unlock()
	if detachedAttachment != nil && detachedAttachment.Mode == "takeover" {
		if _, err := s.restorePostTakeoverTransport(state.attachedTo, detachedAttachment.ReturnTransport); err == nil {
			s.flushQueuedPrompts(state.attachedTo)
		}
	}
	if broadcastSessionID != "" {
		s.broadcastSessionStatus(broadcastSessionID, map[string]any{"attachment_action": "detached"})
	}
}

func (s *Server) waitForConnected(sessionID string) (session.Record, error) {
	deadline := time.Now().Add(s.cfg.StartupTimeout)
	for time.Now().Before(deadline) {
		record, ok := s.registry.Get(sessionID)
		if ok && record.WrapperConnected && record.Runtime.ID != "" {
			return record, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return session.Record{}, fmt.Errorf("timed out waiting for wrapper to connect for session %s", sessionID)
}

func (s *Server) waitForDisconnected(sessionID string) (session.Record, error) {
	deadline := time.Now().Add(s.cfg.StartupTimeout)
	for time.Now().Before(deadline) {
		record, ok := s.registry.Get(sessionID)
		if ok && !record.WrapperConnected {
			return record, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return session.Record{}, fmt.Errorf("timed out waiting for runtime to stop for session %s", sessionID)
}

func (s *Server) ensureTakeoverRuntime(sessionID string, record session.Record) (session.Record, string, error) {
	if record.WrapperConnected && record.Runtime.Transport == runtime.PiPTY().Transport {
		return record, "", nil
	}
	if strings.TrimSpace(record.PersistedSessionHandle) == "" {
		return record, "", nil
	}
	restoredFrom := record.Runtime.Transport
	updated, err := s.replaceRuntimeTransport(sessionID, runtime.PiPTY().Transport)
	if err != nil {
		return session.Record{}, "", err
	}
	if restoredFrom == runtime.PiPTY().Transport {
		restoredFrom = ""
	}
	return updated, restoredFrom, nil
}

func (s *Server) restorePostTakeoverTransport(sessionID, transport string) (session.Record, error) {
	if strings.TrimSpace(transport) == "" {
		record, ok := s.registry.Get(sessionID)
		if !ok {
			return session.Record{}, fmt.Errorf("unknown session")
		}
		return record, nil
	}
	return s.replaceRuntimeTransport(sessionID, transport)
}

func (s *Server) replaceRuntimeTransport(sessionID, transport string) (session.Record, error) {
	record, ok := s.registry.Get(sessionID)
	if !ok {
		return session.Record{}, fmt.Errorf("unknown session")
	}
	descriptor, err := runtimeDescriptorForTransport(transport)
	if err != nil {
		return session.Record{}, err
	}
	if record.WrapperConnected && record.Runtime.Transport == descriptor.Transport {
		return record, nil
	}
	persistedHandle := record.PersistedSessionHandle
	if persistedHandle == "" {
		return session.Record{}, fmt.Errorf("session has no persisted session handle; cannot replace runtime")
	}
	persistedHandle, err = resolveSessionHandle(s.cfg.SessionDir, sessionID, persistedHandle)
	if err != nil {
		return session.Record{}, err
	}

	// Preserve pending prompts across transport replacement to prevent data loss
	var savedPrompts []protocol.Envelope
	if record.WrapperConnected {
		s.mu.Lock()
		if prompts, exists := s.pendingPrompts[sessionID]; exists {
			savedPrompts = append([]protocol.Envelope(nil), prompts...)
		}
		s.mu.Unlock()
	}

	if record.WrapperConnected {
		if err := s.launcher.Stop(sessionID); err != nil {
			return session.Record{}, fmt.Errorf("failed to stop runtime: %v", err)
		}
		if _, err := s.waitForDisconnected(sessionID); err != nil {
			return session.Record{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(persistedHandle), 0o755); err != nil {
		return session.Record{}, err
	}
	s.registry.Ensure(sessionID, persistedHandle)
	if err := s.launcher.Spawn(context.Background(), LaunchRequest{
		SessionID:              sessionID,
		PersistedSessionHandle: persistedHandle,
		RelayURL:               s.relayURL(),
		Token:                  s.cfg.Token,
		PiBin:                  s.cfg.PiBin,
		PTYBin:                 s.cfg.PTYBin,
		RuntimeDescriptor:      descriptor,
	}); err != nil {
		return session.Record{}, errors.New(normalizeSpawnError(err))
	}

	// Restore saved prompts to the new wrapper after it connects
	if len(savedPrompts) > 0 {
		s.mu.Lock()
		s.pendingPrompts[sessionID] = append(s.pendingPrompts[sessionID], savedPrompts...)
		s.mu.Unlock()
	}

	return s.waitForConnected(sessionID)
}

func (s *Server) relayURL() string {
	if s.cfg.PublicURL != "" {
		return s.cfg.PublicURL
	}
	addr := s.cfg.Addr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	addr = strings.Replace(addr, "0.0.0.0:", "127.0.0.1:", 1)
	return "ws://" + addr + "/ws"
}

func runtimeDescriptorForTransport(transport string) (runtime.Descriptor, error) {
	switch strings.TrimSpace(transport) {
	case "", "rpc":
		return runtime.PiRPC(), nil
	case "pty":
		return runtime.PiPTY(), nil
	default:
		return runtime.Descriptor{}, fmt.Errorf("unsupported transport %q", transport)
	}
}

func resolveSessionHandle(sessionDir, sessionID, requested string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}
	baseDir, err := filepath.Abs(sessionDir)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(requested) == "" {
		return filepath.Join(baseDir, sessionID+".jsonl"), nil
	}
	target := requested
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseDir, target)
	}
	target, err = filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	if !pathWithinBase(baseDir, target) {
		return "", fmt.Errorf("session_path must stay within %s", baseDir)
	}
	return target, nil
}

func validateSessionID(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if sessionID == "." || sessionID == ".." || strings.ContainsAny(sessionID, `/\\`) || filepath.Base(sessionID) != sessionID {
		return fmt.Errorf("invalid session_id %q", sessionID)
	}
	return nil
}

func pathWithinBase(baseDir, target string) bool {
	rel, err := filepath.Rel(baseDir, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func mustAckEnvelope(id, sessionID string, record session.Record, command string) protocol.Envelope {
	data := map[string]any{
		"session":                  record.Session,
		"runtime":                  record.Runtime,
		"persisted_session_handle": record.PersistedSessionHandle,
		"wrapper_connected":        record.WrapperConnected,
		"state":                    record.State,
		"phase":                    record.Phase,
		"attachment":               record.Attachment,
		"queued_prompts":           record.QueuedPrompts,
		"pty_rows":                 record.PTYRows,
		"pty_cols":                 record.PTYCols,
		"pending_permissions":      record.PendingPermissions,
		"updated_at":               record.UpdatedAt.Format(time.RFC3339Nano),
	}
	env := mustAckEnvelopeWithData(id, sessionID, command, data)
	env.RuntimeID = record.Runtime.ID
	return env
}

func mustAckEnvelopeWithData(id, sessionID, command string, data map[string]any) protocol.Envelope {
	env, err := protocol.NewEnvelope(protocol.MessageAck, sessionID, "", "weave-relay", id, protocol.AckPayload{
		Command: command,
		Success: true,
		Data:    data,
	})
	if err != nil {
		panic(err)
	}
	return env
}

func normalizeSpawnError(err error) string {
	if errors.Is(err, errRuntimeAlreadyManaged) {
		return "runtime already managed for session"
	}
	return err.Error()
}

func (s *Server) sendError(state *connState, id, sessionID, message string) error {
	env, err := protocol.NewEnvelope(protocol.MessageError, sessionID, "", "weave-relay", id, protocol.ErrorPayload{Error: message})
	if err != nil {
		return err
	}
	return state.writeEnvelope(env)
}

func (s *connState) writeEnvelope(env protocol.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(relayWriteTimeout))
	defer s.conn.SetWriteDeadline(time.Time{})
	return s.conn.WriteJSON(env)
}

type nilWriter struct{}

func (nilWriter) Write(p []byte) (int, error) { return len(p), nil }
