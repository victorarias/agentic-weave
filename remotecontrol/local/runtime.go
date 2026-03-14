package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

type piResponse struct {
	ID      string
	Command string
	Success bool
	Error   string
	Data    map[string]any
}

func (w *Wrapper) startRuntime(ctx context.Context) (<-chan error, func(), error) {
	if w.descriptor.Transport == "pty" {
		return w.startPTYRuntime(ctx)
	}
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
				select {
				case ch <- resp:
				default:
				}
			}
			return nil
		}
		if updates := w.normalizeUIRequest(msg); len(updates) > 0 {
			for _, update := range updates {
				w.broadcastUpdate(update)
			}
			return nil
		}
		for _, update := range normalizePIEvent(msg) {
			w.broadcastUpdate(update)
		}
		return nil
	})
}

func (w *Wrapper) normalizeUIRequest(msg map[string]any) []protocol.SessionUpdate {
	if stringValue(msg["type"]) != "extension_ui_request" {
		return nil
	}
	if stringValue(msg["method"]) != "confirm" {
		return nil
	}
	requestID := stringValue(msg["id"])
	if requestID == "" {
		return nil
	}
	permission := protocol.PermissionRequest{
		ID:        requestID,
		Kind:      "confirm",
		Title:     stringValue(msg["title"]),
		Message:   stringValue(msg["message"]),
		Options:   []string{"allow", "deny"},
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Raw:       msg,
	}
	w.permissionMu.Lock()
	w.pendingPermission[requestID] = permission
	w.permissionMu.Unlock()
	return []protocol.SessionUpdate{
		{Kind: protocol.UpdatePermissionRequest, RequestID: requestID, Message: permission.Title, Permission: &permission, Details: map[string]any{"permission": permission}},
		{Kind: protocol.UpdateStatus, Phase: "waiting_permission", Details: map[string]any{"run_state": "waiting_permission", "request_id": requestID}},
	}
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
