package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	Version = "weave-rc/0.1"

	MessageCommand = "command"
	MessageAck     = "ack"
	MessageEvent   = "event"
	MessageError   = "error"

	CommandAuth                 = "auth"
	CommandInitialize           = "initialize"
	CommandSessionStatus        = "session.status"
	CommandSessionSpawn         = "session.spawn"
	CommandSessionLoad          = "session.load"
	CommandRuntimeStop          = "runtime.stop"
	CommandRegistryListSessions = "registry.list_sessions"
	CommandSessionPrompt        = "session.prompt"
	CommandSessionCancel        = "session.cancel"

	RoleWrapper = "wrapper"
	RoleClient  = "client"

	EventSessionAgentReady = "session.agent_ready"
	EventSessionUpdate     = "session.update"

	UpdateLifecycle       = "lifecycle"
	UpdateMessageDelta    = "message_delta"
	UpdateMessageComplete = "message_complete"
	UpdateToolBegin       = "tool_begin"
	UpdateToolEnd         = "tool_end"
	UpdateError           = "error"
	UpdateComplete        = "complete"
)

type Envelope struct {
	ID        string          `json:"id,omitempty"`
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	RuntimeID string          `json:"runtime_id,omitempty"`
	From      string          `json:"from,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type AuthCommand struct {
	Command string `json:"command"`
	Token   string `json:"token"`
	Role    string `json:"role"`
}

type InitializeCommand struct {
	Command         string          `json:"command"`
	ProtocolVersion string          `json:"protocol_version"`
	Capabilities    map[string]bool `json:"capabilities,omitempty"`
}

type SessionStatusCommand struct {
	Command string `json:"command"`
}

type SessionSpawnCommand struct {
	Command     string `json:"command"`
	SessionPath string `json:"session_path,omitempty"`
}

type SessionLoadCommand struct {
	Command     string `json:"command"`
	SessionPath string `json:"session_path,omitempty"`
}

type RuntimeStopCommand struct {
	Command string `json:"command"`
}

type ListSessionsCommand struct {
	Command string `json:"command"`
}

type SessionPromptCommand struct {
	Command  string `json:"command"`
	Message  string `json:"message"`
	Delivery string `json:"delivery,omitempty"`
}

type SessionCancelCommand struct {
	Command string `json:"command"`
}

type AckPayload struct {
	Command string         `json:"command"`
	Success bool           `json:"success"`
	Data    map[string]any `json:"data,omitempty"`
}

type ErrorPayload struct {
	Command string `json:"command,omitempty"`
	Error   string `json:"error"`
}

type AgentReadyEvent struct {
	Event           string          `json:"event"`
	ProtocolVersion string          `json:"protocol_version"`
	Capabilities    map[string]bool `json:"capabilities,omitempty"`
	Session         SessionInfo     `json:"session"`
	Runtime         RuntimeInfo     `json:"runtime"`
}

type SessionInfo struct {
	ID string `json:"id"`
}

type RuntimeInfo struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Transport string `json:"transport"`
}

type SessionUpdateEvent struct {
	Event  string        `json:"event"`
	Update SessionUpdate `json:"update"`
}

type SessionUpdate struct {
	Kind       string         `json:"kind"`
	Phase      string         `json:"phase,omitempty"`
	Delta      string         `json:"delta,omitempty"`
	Message    string         `json:"message,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	IsError    bool           `json:"is_error,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

func NewEnvelope(messageType, sessionID, runtimeID, from, id string, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		ID:        id,
		Type:      messageType,
		SessionID: sessionID,
		RuntimeID: runtimeID,
		From:      from,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Payload:   raw,
	}, nil
}

func (e Envelope) DecodePayload(dst any) error {
	if len(e.Payload) == 0 {
		return io.EOF
	}
	return json.Unmarshal(e.Payload, dst)
}

func WriteJSONLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func ReadJSONL(r io.Reader, fn func([]byte) error) error {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSuffix(line, []byte{'\n'})
			line = bytes.TrimSuffix(line, []byte{'\r'})
			if len(bytes.TrimSpace(line)) > 0 {
				if callErr := fn(line); callErr != nil {
					return callErr
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func DecodeEnvelope(line []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return Envelope{}, err
	}
	if strings.TrimSpace(env.Type) == "" {
		return Envelope{}, fmt.Errorf("protocol: missing envelope type")
	}
	return env, nil
}
