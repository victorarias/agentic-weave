package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "weave-inspect:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: weave-inspect <local|relay> [flags] <init|sessions|status|spawn|load|kill-runtime|attach|detach|inject|prompt|cancel|allow|deny> [message]")
	}
	switch os.Args[1] {
	case "local":
		return runLocal(os.Args[2:])
	case "relay":
		return runRelay(os.Args[2:])
	default:
		return fmt.Errorf("usage: weave-inspect <local|relay> [flags] <init|sessions|status|spawn|load|kill-runtime|attach|detach|inject|prompt|cancel|allow|deny> [message]")
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
	if subcmd == "sessions" || subcmd == "spawn" || subcmd == "load" || subcmd == "kill-runtime" || subcmd == "attach" || subcmd == "detach" || subcmd == "inject" {
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
	return client.execute(subcmd, message, *delivery, *sessionID, *jsonMode)
}

func runRelay(args []string) error {
	fs := flag.NewFlagSet("weave-inspect relay", flag.ContinueOnError)
	relayURL := fs.String("relay", "ws://localhost:8080/ws", "Relay websocket URL")
	token := fs.String("token", "", "Shared bearer token")
	sessionID := fs.String("session", "local", "Logical session id")
	jsonMode := fs.Bool("json", false, "Print raw JSON envelopes")
	delivery := fs.String("delivery", "", "Prompt delivery mode: default, foreground, interrupt, queue, deliver_when_idle")
	identity := fs.String("identity", "weave-inspect", "Client identity used for attach/inject authority")
	if err := fs.Parse(args); err != nil {
		return err
	}
	subcmd, message, err := parseSubcommand(fs.Args())
	if err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.Dial(*relayURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := newRelayClient(conn, *token, *identity)
	if err := client.authenticate(*jsonMode); err != nil {
		return err
	}
	if shouldInitializeRelay(subcmd) {
		if err := client.initialize(*sessionID, *jsonMode); err != nil {
			return err
		}
	}
	return client.execute(subcmd, message, *delivery, *sessionID, *jsonMode)
}

func parseSubcommand(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("missing subcommand: init, sessions, status, spawn, load, kill-runtime, attach, detach, inject, prompt, cancel, allow, or deny")
	}
	subcmd := args[0]
	switch subcmd {
	case "init", "sessions", "status", "spawn", "load", "kill-runtime", "detach", "cancel":
		return subcmd, "", nil
	case "attach":
		if len(args) < 2 {
			return "", "", fmt.Errorf("attach requires a mode: observe or inject")
		}
		return subcmd, args[1], nil
	case "inject":
		if len(args) < 2 {
			return "", "", fmt.Errorf("inject requires a message")
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

func (c *localClient) execute(subcmd, message, delivery, sessionID string, jsonMode bool) error {
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
	case "sessions", "spawn", "load", "kill-runtime", "attach", "detach", "inject":
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
	conn     *websocket.Conn
	token    string
	identity string
	stream   *envelopeStream
	errCh    chan error
}

func newRelayClient(conn *websocket.Conn, token, identity string) *relayClient {
	events := make(chan protocol.Envelope, 64)
	c := &relayClient{conn: conn, token: token, identity: identity, stream: newEnvelopeStream(events), errCh: make(chan error, 1)}
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

func (c *relayClient) execute(subcmd, message, delivery, sessionID string, jsonMode bool) error {
	switch subcmd {
	case "init":
		return nil
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
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "spawn-1", protocol.SessionSpawnCommand{Command: protocol.CommandSessionSpawn})
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
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "load-1", protocol.SessionLoadCommand{Command: protocol.CommandSessionLoad})
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
	case "attach":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "attach-1", protocol.SessionAttachCommand{Command: protocol.CommandSessionAttach, Mode: message})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, "attach-1", jsonMode)
		if err != nil {
			return err
		}
		if err := printStatus(ack, jsonMode); err != nil {
			return err
		}
		streamErr := streamUntilInterrupt(c.stream, c.errCh, jsonMode)
		detachEnv, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "detach-1", protocol.SessionDetachCommand{Command: protocol.CommandSessionDetach})
		if err == nil {
			if err := c.conn.WriteJSON(detachEnv); err == nil {
				_, _ = waitForAckEnvelope(c.stream, "detach-1", jsonMode)
			}
		}
		return streamErr
	case "detach":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", c.identity, "detach-1", protocol.SessionDetachCommand{Command: protocol.CommandSessionDetach})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		ack, err := waitForAckEnvelope(c.stream, "detach-1", jsonMode)
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
		if _, err := waitForAckEnvelope(c.stream, "prompt-1", jsonMode); err != nil {
			return err
		}
		return streamUntilComplete(c.stream, c.errCh, jsonMode)
	default:
		return fmt.Errorf("unknown subcommand %q", subcmd)
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
		attachmentID := ""
		attachmentMode := ""
		if attachment, ok := row["attachment"].(map[string]any); ok {
			attachmentID = stringValue(attachment["client_id"])
			attachmentMode = stringValue(attachment["mode"])
		}
		fmt.Fprintf(os.Stdout, "session_id=%s runtime_id=%s state=%s phase=%s wrapper_connected=%v pending_permissions=%d attached_client_id=%s attached_mode=%s\n",
			stringValue(sessionMap["id"]),
			stringValue(runtimeMap["id"]),
			stringValue(row["state"]),
			stringValue(row["phase"]),
			row["wrapper_connected"],
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
		if env.Type != protocol.MessageEvent {
			continue
		}
		var evt protocol.SessionUpdateEvent
		if err := env.DecodePayload(&evt); err != nil || evt.Event != protocol.EventSessionUpdate {
			continue
		}
		switch evt.Update.Kind {
		case protocol.UpdateMessageDelta:
			fmt.Fprint(os.Stdout, evt.Update.Delta)
		case protocol.UpdateMessageComplete:
			if evt.Update.Message != "" {
				fmt.Fprintln(os.Stdout)
			}
		case protocol.UpdateToolBegin:
			fmt.Fprintf(os.Stderr, "\n[tool start] %s\n", evt.Update.ToolName)
		case protocol.UpdateToolEnd:
			fmt.Fprintf(os.Stderr, "\n[tool end] %s\n", evt.Update.ToolName)
		case protocol.UpdatePermissionRequest:
			title := evt.Update.Message
			if evt.Update.Permission != nil && evt.Update.Permission.Title != "" {
				title = evt.Update.Permission.Title
			}
			fmt.Fprintf(os.Stderr, "\n[permission request] id=%s title=%s\n", evt.Update.RequestID, title)
		case protocol.UpdatePermissionResolved:
			fmt.Fprintf(os.Stderr, "\n[permission resolved] id=%s decision=%s\n", evt.Update.RequestID, evt.Update.Decision)
		case protocol.UpdateStatus:
			if evt.Update.Phase != "" {
				fmt.Fprintf(os.Stderr, "\n[status] %s\n", evt.Update.Phase)
			}
		case protocol.UpdateError:
			return errors.New(evt.Update.Message)
		case protocol.UpdateComplete:
			fmt.Fprintln(os.Stdout)
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
		if env.Type != protocol.MessageEvent {
			continue
		}
		var evt protocol.SessionUpdateEvent
		if err := env.DecodePayload(&evt); err != nil || evt.Event != protocol.EventSessionUpdate {
			continue
		}
		switch evt.Update.Kind {
		case protocol.UpdateMessageDelta:
			fmt.Fprint(os.Stdout, evt.Update.Delta)
		case protocol.UpdateMessageComplete:
			if evt.Update.Message != "" {
				fmt.Fprintln(os.Stdout)
			}
		case protocol.UpdateToolBegin:
			fmt.Fprintf(os.Stderr, "\n[tool start] %s\n", evt.Update.ToolName)
		case protocol.UpdateToolEnd:
			fmt.Fprintf(os.Stderr, "\n[tool end] %s\n", evt.Update.ToolName)
		case protocol.UpdatePermissionRequest:
			title := evt.Update.Message
			if evt.Update.Permission != nil && evt.Update.Permission.Title != "" {
				title = evt.Update.Permission.Title
			}
			fmt.Fprintf(os.Stderr, "\n[permission request] id=%s title=%s\n", evt.Update.RequestID, title)
		case protocol.UpdatePermissionResolved:
			fmt.Fprintf(os.Stderr, "\n[permission resolved] id=%s decision=%s\n", evt.Update.RequestID, evt.Update.Decision)
		case protocol.UpdateStatus:
			if evt.Update.Phase != "" {
				fmt.Fprintf(os.Stderr, "\n[status] %s\n", evt.Update.Phase)
			}
		case protocol.UpdateError:
			return errors.New(evt.Update.Message)
		case protocol.UpdateComplete:
			fmt.Fprintln(os.Stdout)
		}
	}
}

func shouldInitializeRelay(subcmd string) bool {
	switch subcmd {
	case "init", "attach", "detach", "inject", "prompt", "cancel", "allow", "deny":
		return true
	default:
		return false
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
