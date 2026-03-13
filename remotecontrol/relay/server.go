package relay

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
	"github.com/victorarias/agentic-weave/remotecontrol/session"
)

type Config struct {
	Addr   string
	Token  string
	Logger *log.Logger
}

type Server struct {
	cfg      Config
	upgrader websocket.Upgrader

	mu          sync.Mutex
	wrappers    map[string]*connState
	subscribers map[string]map[*connState]struct{}
	registry    *session.Registry
}

type connState struct {
	conn      *websocket.Conn
	role      string
	sessionID string
	runtimeID string
	mu        sync.Mutex
	authed    bool
	sessions  map[string]struct{}
}

func NewServer(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = log.New(nilWriter{}, "", 0)
	}
	return &Server{
		cfg: cfg,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		wrappers:    make(map[string]*connState),
		subscribers: make(map[string]map[*connState]struct{}),
		registry:    session.NewRegistry(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/", s.handleWS)
	return mux
}

func (s *Server) Run(ctx context.Context) error {
	if s.cfg.Addr == "" {
		s.cfg.Addr = ":8080"
	}
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
		s.registry.SetConnected(state.sessionID, protocol.RuntimeInfo{ID: state.runtimeID, Kind: "pi", Transport: "rpc"})
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
		_ = s.sendError(state, env.ID, env.SessionID, "session_id is required")
		return
	}

	var meta struct {
		Command string `json:"command"`
	}
	if err := env.DecodePayload(&meta); err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	if meta.Command == protocol.CommandSessionStatus {
		s.handleSessionStatus(state, env)
		return
	}

	s.subscribe(state, env.SessionID)

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

func (s *Server) handleSessionStatus(state *connState, env protocol.Envelope) {
	record, ok := s.registry.Get(env.SessionID)
	if !ok {
		_ = s.sendError(state, env.ID, env.SessionID, "unknown session")
		return
	}
	ack, err := protocol.NewEnvelope(protocol.MessageAck, env.SessionID, record.Runtime.ID, "weave-relay", env.ID, protocol.AckPayload{
		Command: protocol.CommandSessionStatus,
		Success: true,
		Data: map[string]any{
			"session":           record.Session,
			"runtime":           record.Runtime,
			"wrapper_connected": record.WrapperConnected,
			"updated_at":        record.UpdatedAt.Format(time.RFC3339Nano),
		},
	})
	if err != nil {
		_ = s.sendError(state, env.ID, env.SessionID, err.Error())
		return
	}
	_ = state.writeEnvelope(ack)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if state.role == protocol.RoleWrapper && state.sessionID != "" {
		if s.wrappers[state.sessionID] == state {
			delete(s.wrappers, state.sessionID)
			s.registry.SetDisconnected(state.sessionID, state.runtimeID)
		}
	}
	for sessionID := range state.sessions {
		subs := s.subscribers[sessionID]
		delete(subs, state)
		if len(subs) == 0 {
			delete(s.subscribers, sessionID)
		}
	}
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
