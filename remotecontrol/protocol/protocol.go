package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
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

	CommandAuth                      = "auth"
	CommandInitialize                = "initialize"
	CommandSessionStatus             = "session.status"
	CommandSessionSpawn              = "session.spawn"
	CommandSessionLoad               = "session.load"
	CommandRuntimeStop               = "runtime.stop"
	CommandRuntimeReplace            = "runtime.replace"
	CommandRegistryListSessions      = "registry.list_sessions"
	CommandSessionPermissionResponse = "session.permission_response"
	CommandSessionAttach             = "session.attach"
	CommandSessionDetach             = "session.detach"
	CommandSessionPrompt             = "session.prompt"
	CommandSessionCancel             = "session.cancel"
	CommandPTYInput                  = "pty.input"
	CommandPTYResize                 = "pty.resize"

	RoleWrapper = "wrapper"
	RoleClient  = "client"

	EventSessionAgentReady = "session.agent_ready"
	EventSessionUpdate     = "session.update"
	EventPTYOutput         = "pty.output"

	UpdateLifecycle          = "lifecycle"
	UpdateMessageDelta       = "message_delta"
	UpdateMessageComplete    = "message_complete"
	UpdateToolBegin          = "tool_begin"
	UpdateToolEnd            = "tool_end"
	UpdatePermissionRequest  = "permission_request"
	UpdatePermissionResolved = "permission_resolved"
	UpdateStatus             = "status"
	UpdateError              = "error"
	UpdateComplete           = "complete"

	MaxJSONLLineBytes = 1 << 20
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
	Command   string `json:"command"`
	Token     string `json:"token"`
	Role      string `json:"role"`
	Transport string `json:"transport,omitempty"`
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
	Transport   string `json:"transport,omitempty"`
}

type SessionLoadCommand struct {
	Command     string `json:"command"`
	SessionPath string `json:"session_path,omitempty"`
	Transport   string `json:"transport,omitempty"`
}

type RuntimeStopCommand struct {
	Command string `json:"command"`
}

type RuntimeReplaceCommand struct {
	Command   string `json:"command"`
	Transport string `json:"transport,omitempty"`
}

type ListSessionsCommand struct {
	Command string `json:"command"`
}

type PermissionResponseCommand struct {
	Command   string `json:"command"`
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"`
}

type SessionAttachCommand struct {
	Command string `json:"command"`
	Mode    string `json:"mode"`
}

type SessionDetachCommand struct {
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

type PTYInputCommand struct {
	Command string `json:"command"`
	Data    string `json:"data"`
}

type PTYResizeCommand struct {
	Command string `json:"command"`
	Rows    int    `json:"rows"`
	Cols    int    `json:"cols"`
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

type AttachmentInfo struct {
	ClientID string `json:"client_id"`
	Mode     string `json:"mode"`
}

type SessionUpdateEvent struct {
	Event  string        `json:"event"`
	Update SessionUpdate `json:"update"`
}

type PTYOutputEvent struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

type PermissionRequest struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Title     string         `json:"title,omitempty"`
	Message   string         `json:"message,omitempty"`
	Options   []string       `json:"options,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	Raw       map[string]any `json:"raw,omitempty"`
}

type SessionUpdate struct {
	Kind       string             `json:"kind"`
	Phase      string             `json:"phase,omitempty"`
	Delta      string             `json:"delta,omitempty"`
	Message    string             `json:"message,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	ToolName   string             `json:"tool_name,omitempty"`
	RequestID  string             `json:"request_id,omitempty"`
	Decision   string             `json:"decision,omitempty"`
	Permission *PermissionRequest `json:"permission,omitempty"`
	IsError    bool               `json:"is_error,omitempty"`
	Details    map[string]any     `json:"details,omitempty"`
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
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxJSONLLineBytes)
	for scanner.Scan() {
		line := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if callErr := fn(line); callErr != nil {
			return callErr
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("protocol: jsonl line exceeds %d bytes", MaxJSONLLineBytes)
		}
		return err
	}
	return nil
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
