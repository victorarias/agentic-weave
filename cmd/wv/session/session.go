package session

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/history"
	"github.com/victorarias/agentic-weave/agentic/loop"
)

const (
	UpdateRunStart = "run_start"
	UpdateRunEnd   = "run_end"
	UpdateRunError = "run_error"
	UpdateEvent    = "event"
)

// Update is emitted by Session and consumed by the TUI.
type Update struct {
	Type   string
	Event  events.Event
	Result *loop.Result
	Err    error
}

// Config wires the session runtime.
type Config struct {
	Decider      loop.Decider
	Executor     agentic.ToolExecutor
	HistoryStore history.Store
	SystemPrompt string
	MaxTurns     int
}

// Session manages asynchronous loop execution and event forwarding.
type Session struct {
	runner       *loop.Runner
	systemPrompt string

	updates chan Update

	mu      sync.Mutex
	running bool
}

// New constructs a new session.
func New(cfg Config) (*Session, error) {
	if cfg.HistoryStore == nil {
		cfg.HistoryStore = history.NewMemoryStore()
	}
	if cfg.Decider == nil {
		return nil, errors.New("session: decider is required")
	}

	s := &Session{
		systemPrompt: cfg.SystemPrompt,
		updates:      make(chan Update, 512),
	}

	sink := events.SinkFunc(func(e events.Event) {
		s.emitUpdate(Update{Type: UpdateEvent, Event: e})
	})

	s.runner = loop.New(loop.Config{
		Decider:      cfg.Decider,
		Executor:     cfg.Executor,
		HistoryStore: cfg.HistoryStore,
		Events:       sink,
		MaxTurns:     cfg.MaxTurns,
	})

	return s, nil
}

// Updates returns the session update stream.
func (s *Session) Updates() <-chan Update {
	return s.updates
}

// Send starts an asynchronous run for the provided user message.
func (s *Session) Send(ctx context.Context, userMessage string) error {
	msg := strings.TrimSpace(userMessage)
	if msg == "" {
		return errors.New("session: empty message")
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("session: run already in progress")
	}
	s.running = true
	s.mu.Unlock()

	s.emitUpdate(Update{Type: UpdateRunStart})

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()

		result, err := s.runner.Run(ctx, loop.Request{
			SystemPrompt: s.systemPrompt,
			UserMessage:  msg,
		})
		if err != nil {
			s.emitUpdate(Update{Type: UpdateRunError, Err: err})
			return
		}
		s.emitUpdate(Update{Type: UpdateRunEnd, Result: &result})
	}()

	return nil
}

func (s *Session) emitUpdate(u Update) {
	// Never drop lifecycle updates; blocking here preserves run_start/run_end/run_error ordering.
	if u.Type != UpdateEvent {
		s.updates <- u
		return
	}
	if shouldDropUnderPressure(u.Event.Type) {
		// High-frequency deltas are best-effort and can be dropped under UI backpressure.
		select {
		case s.updates <- u:
		default:
		}
		return
	}
	// Structural events are blocking to preserve coherent UI state.
	s.updates <- u
}

func shouldDropUnderPressure(eventType string) bool {
	switch eventType {
	case events.MessageUpdate:
		return true
	default:
		return false
	}
}
