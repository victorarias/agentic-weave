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
	StartupTimeout time.Duration
	Launcher       Launcher
}

type Server struct {
	cfg      Config
	upgrader websocket.Upgrader

	mu          sync.Mutex
	wrappers    map[string]*connState
	attachments map[string]*connState
	subscribers map[string]map[*connState]struct{}
	registry    *session.Registry
	launcher    Launcher
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
		wrappers:    make(map[string]*connState),
		attachments: make(map[string]*connState),
		subscribers: make(map[string]map[*connState]struct{}),
		registry:    session.NewRegistry(),
		launcher:    launcher,
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
	if cmd.Role == protocol.RoleWrapper && env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "wrapper auth requires session_id")
		return
	}
	if cmd.Role == protocol.RoleWrapper && env.RuntimeID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "wrapper auth requires runtime_id")
		return
	}

	state.authed = true
	state.role = cmd.Role
	state.identity = env.From
	if state.identity == "" {
		state.identity = cmd.Role
	}
	state.sessionID = env.SessionID
	state.runtimeID = env.RuntimeID

	s.mu.Lock()
	if state.role == protocol.RoleWrapper {
		if existing := s.wrappers[state.sessionID]; existing != nil {
			s.mu.Unlock()
			_ = s.sendError(state, env.ID, env.SessionID, "wrapper already registered for session")
			return
		}
		s.wrappers[state.sessionID] = state
		s.registry.SetConnected(state.sessionID, protocol.RuntimeInfo{ID: state.runtimeID, Kind: "pi", Transport: runtime.PiRPC().Transport}, "")
	}
	s.mu.Unlock()

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
	case protocol.CommandSessionPermissionResponse:
		s.handlePermissionResponse(state, env)
		return
	case protocol.CommandSessionAttach:
		s.handleSessionAttach(state, env)
		return
	case protocol.CommandSessionDetach:
		s.handleSessionDetach(state, env)
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
	s.applyWrapperEnvelope(state, env)

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
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for session.spawn")
		return
	}
	if record, ok := s.registry.Get(env.SessionID); ok && record.WrapperConnected {
		_ = s.sendError(state, env.ID, env.SessionID, "session already has a connected runtime")
		return
	}
	persistedHandle := cmd.SessionPath
	if persistedHandle == "" {
		persistedHandle = filepath.Join(s.cfg.SessionDir, env.SessionID+".jsonl")
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
		RuntimeDescriptor:      runtime.PiRPC(),
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
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for session.load")
		return
	}
	if record, ok := s.registry.Get(env.SessionID); ok && record.WrapperConnected {
		_ = s.sendError(state, env.ID, env.SessionID, "session already has a connected runtime")
		return
	}
	record, _ := s.registry.Get(env.SessionID)
	persistedHandle := cmd.SessionPath
	if persistedHandle == "" {
		persistedHandle = record.PersistedSessionHandle
	}
	if persistedHandle == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session.load requires a known persisted session handle")
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
		RuntimeDescriptor:      runtime.PiRPC(),
	}); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, normalizeSpawnError(err))
		return
	}
	record, err := s.waitForConnected(env.SessionID)
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
	if cmd.Mode != "observe" && cmd.Mode != "inject" {
		_ = s.sendError(state, env.ID, env.SessionID, fmt.Sprintf("unsupported attach mode %q", cmd.Mode))
		return
	}
	if state.identity == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "attach requires client identity")
		return
	}
	if _, ok := s.registry.Get(env.SessionID); !ok {
		_ = s.sendError(state, env.ID, env.SessionID, "unknown session")
		return
	}

	var previousSessionID string
	s.mu.Lock()
	attached := s.attachments[env.SessionID]
	if attached != nil && attached != state {
		s.mu.Unlock()
		_ = s.sendError(state, env.ID, env.SessionID, "another controller is already attached")
		return
	}
	if state.attachedTo != "" && state.attachedTo != env.SessionID {
		previousSessionID = state.attachedTo
		if s.attachments[state.attachedTo] == state {
			delete(s.attachments, state.attachedTo)
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
	record, _ := s.registry.SetAttachment(env.SessionID, protocol.AttachmentInfo{ClientID: state.identity, Mode: cmd.Mode})
	_ = state.writeEnvelope(mustAckEnvelope(env.ID, env.SessionID, record, protocol.CommandSessionAttach))
	if previousSessionID != "" {
		s.broadcastSessionStatus(previousSessionID)
	}
	s.broadcastSessionStatus(env.SessionID)
}

func (s *Server) handleSessionDetach(state *connState, env protocol.Envelope) {
	if env.SessionID == "" {
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required for session.detach")
		return
	}
	cleared, record, err := s.detachIfOwner(state, env.SessionID)
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if !cleared {
		_ = s.sendError(state, env.ID, env.SessionID, "client is not attached to session")
		return
	}
	_ = state.writeEnvelope(mustAckEnvelope(env.ID, env.SessionID, record, protocol.CommandSessionDetach))
	s.broadcastSessionStatus(env.SessionID)
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
	if record.Attachment.Mode != "inject" {
		return true
	}
	return record.Attachment.ClientID == state.identity
}

func (s *Server) detachIfOwner(state *connState, sessionID string) (bool, session.Record, error) {
	s.mu.Lock()
	owner := s.attachments[sessionID]
	if owner != state {
		s.mu.Unlock()
		return false, session.Record{}, nil
	}
	delete(s.attachments, sessionID)
	state.attachedTo = ""
	state.attachedMode = ""
	s.mu.Unlock()
	record, ok := s.registry.ClearAttachment(sessionID)
	if !ok {
		return false, session.Record{}, fmt.Errorf("unknown session")
	}
	return true, record, nil
}

func (s *Server) broadcastSessionStatus(sessionID string) {
	record, ok := s.registry.Get(sessionID)
	if !ok {
		return
	}
	phase := record.Phase
	if phase == "" {
		phase = record.State
	}
	update := protocol.SessionUpdate{Kind: protocol.UpdateStatus, Phase: phase, Details: map[string]any{"state": record.State}}
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
	s.mu.Lock()
	if state.role == protocol.RoleWrapper && state.sessionID != "" {
		if s.wrappers[state.sessionID] == state {
			delete(s.wrappers, state.sessionID)
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
	if broadcastSessionID != "" {
		s.broadcastSessionStatus(broadcastSessionID)
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

func mustAckEnvelope(id, sessionID string, record session.Record, command string) protocol.Envelope {
	env, err := protocol.NewEnvelope(protocol.MessageAck, sessionID, record.Runtime.ID, "weave-relay", id, protocol.AckPayload{
		Command: command,
		Success: true,
		Data: map[string]any{
			"session":                  record.Session,
			"runtime":                  record.Runtime,
			"persisted_session_handle": record.PersistedSessionHandle,
			"wrapper_connected":        record.WrapperConnected,
			"state":                    record.State,
			"phase":                    record.Phase,
			"attachment":               record.Attachment,
			"pending_permissions":      record.PendingPermissions,
			"updated_at":               record.UpdatedAt.Format(time.RFC3339Nano),
		},
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
	return s.conn.WriteJSON(env)
}

type nilWriter struct{}

func (nilWriter) Write(p []byte) (int, error) { return len(p), nil }
