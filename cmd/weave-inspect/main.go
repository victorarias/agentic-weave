package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
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
		return fmt.Errorf("usage: weave-inspect <local|relay> [flags] <init|prompt|cancel> [message]")
	}
	switch os.Args[1] {
	case "local":
		return runLocal(os.Args[2:])
	case "relay":
		return runRelay(os.Args[2:])
	default:
		return fmt.Errorf("usage: weave-inspect <local|relay> [flags] <init|prompt|cancel> [message]")
	}
}

func runLocal(args []string) error {
	fs := flag.NewFlagSet("weave-inspect local", flag.ContinueOnError)
	socket := fs.String("socket", "/tmp/weave-local.sock", "Unix socket path")
	sessionID := fs.String("session", "local", "Logical session id")
	jsonMode := fs.Bool("json", false, "Print raw JSON envelopes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	subcmd, message, err := parseSubcommand(fs.Args())
	if err != nil {
		return err
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
	return client.execute(subcmd, message, *sessionID, *jsonMode)
}

func runRelay(args []string) error {
	fs := flag.NewFlagSet("weave-inspect relay", flag.ContinueOnError)
	relayURL := fs.String("relay", "ws://localhost:8080/ws", "Relay websocket URL")
	token := fs.String("token", "", "Shared bearer token")
	sessionID := fs.String("session", "local", "Logical session id")
	jsonMode := fs.Bool("json", false, "Print raw JSON envelopes")
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

	client := newRelayClient(conn, *token)
	if err := client.authenticate(*jsonMode); err != nil {
		return err
	}
	if err := client.initialize(*sessionID, *jsonMode); err != nil {
		return err
	}
	return client.execute(subcmd, message, *sessionID, *jsonMode)
}

func parseSubcommand(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("missing subcommand: init, prompt, or cancel")
	}
	subcmd := args[0]
	switch subcmd {
	case "init", "cancel":
		return subcmd, "", nil
	case "prompt":
		if len(args) < 2 {
			return "", "", fmt.Errorf("prompt requires a message")
		}
		return subcmd, strings.Join(args[1:], " "), nil
	default:
		return "", "", fmt.Errorf("unknown subcommand %q", subcmd)
	}
}

type inspectClient interface {
	initialize(sessionID string, jsonMode bool) error
	execute(subcmd, message, sessionID string, jsonMode bool) error
}

type localClient struct {
	conn   net.Conn
	events chan protocol.Envelope
	errCh  chan error
}

func newLocalClient(conn net.Conn) *localClient {
	c := &localClient{conn: conn, events: make(chan protocol.Envelope, 64), errCh: make(chan error, 1)}
	go func() {
		c.errCh <- protocol.ReadJSONL(conn, func(line []byte) error {
			env, err := protocol.DecodeEnvelope(line)
			if err != nil {
				return err
			}
			c.events <- env
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
	return waitForInit(c.events, jsonMode, "init-1")
}

func (c *localClient) execute(subcmd, message, sessionID string, jsonMode bool) error {
	switch subcmd {
	case "init":
		return nil
	case "cancel":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "cancel-1", protocol.SessionCancelCommand{Command: protocol.CommandSessionCancel})
		if err != nil {
			return err
		}
		if err := protocol.WriteJSONLine(c.conn, env); err != nil {
			return err
		}
		return waitForAckOrError(c.events, "cancel-1", jsonMode)
	case "prompt":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: message})
		if err != nil {
			return err
		}
		if err := protocol.WriteJSONLine(c.conn, env); err != nil {
			return err
		}
		if err := waitForAckOrError(c.events, "prompt-1", jsonMode); err != nil {
			return err
		}
		return streamUntilComplete(c.events, c.errCh, jsonMode)
	default:
		return fmt.Errorf("unknown subcommand %q", subcmd)
	}
}

type relayClient struct {
	conn   *websocket.Conn
	token  string
	events chan protocol.Envelope
	errCh  chan error
}

func newRelayClient(conn *websocket.Conn, token string) *relayClient {
	c := &relayClient{conn: conn, token: token, events: make(chan protocol.Envelope, 64), errCh: make(chan error, 1)}
	go func() {
		for {
			var env protocol.Envelope
			if err := conn.ReadJSON(&env); err != nil {
				c.errCh <- err
				return
			}
			c.events <- env
		}
	}()
	return c
}

func (c *relayClient) authenticate(jsonMode bool) error {
	env, err := protocol.NewEnvelope(protocol.MessageCommand, "", "", "weave-inspect", "auth-1", protocol.AuthCommand{
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
	return waitForAckOrError(c.events, "auth-1", jsonMode)
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
		if err := waitForInit(c.events, jsonMode, requestID); err != nil {
			if strings.Contains(err.Error(), "no wrapper connected for session") && time.Now().Before(deadline) {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return err
		}
		return nil
	}
}

func (c *relayClient) execute(subcmd, message, sessionID string, jsonMode bool) error {
	switch subcmd {
	case "init":
		return nil
	case "cancel":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "cancel-1", protocol.SessionCancelCommand{Command: protocol.CommandSessionCancel})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		return waitForAckOrError(c.events, "cancel-1", jsonMode)
	case "prompt":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, sessionID, "", "weave-inspect", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: message})
		if err != nil {
			return err
		}
		if err := c.conn.WriteJSON(env); err != nil {
			return err
		}
		if err := waitForAckOrError(c.events, "prompt-1", jsonMode); err != nil {
			return err
		}
		return streamUntilComplete(c.events, c.errCh, jsonMode)
	default:
		return fmt.Errorf("unknown subcommand %q", subcmd)
	}
}

func waitForInit(events <-chan protocol.Envelope, jsonMode bool, requestID string) error {
	deadline := time.After(10 * time.Second)
	seenAck := false
	seenReady := false
	for !(seenAck && seenReady) {
		select {
		case env := <-events:
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
			}
			if env.Type == protocol.MessageEvent {
				var evt protocol.AgentReadyEvent
				if err := env.DecodePayload(&evt); err == nil && evt.Event == protocol.EventSessionAgentReady {
					seenReady = true
				}
			}
		case <-deadline:
			return fmt.Errorf("timed out waiting for initialize")
		}
	}
	return nil
}

func waitForAckOrError(events <-chan protocol.Envelope, id string, jsonMode bool) error {
	deadline := time.After(10 * time.Second)
	for {
		select {
		case env := <-events:
			if jsonMode {
				_ = protocol.WriteJSONLine(os.Stdout, env)
			}
			if env.ID != id {
				continue
			}
			switch env.Type {
			case protocol.MessageAck:
				return nil
			case protocol.MessageError:
				var payload protocol.ErrorPayload
				if err := env.DecodePayload(&payload); err != nil {
					return err
				}
				return errors.New(payload.Error)
			}
		case <-deadline:
			return fmt.Errorf("timed out waiting for %s", id)
		}
	}
}

func streamUntilComplete(events <-chan protocol.Envelope, errCh <-chan error, jsonMode bool) error {
	for {
		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
			return nil
		case env := <-events:
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
			case protocol.UpdateError:
				return errors.New(evt.Update.Message)
			case protocol.UpdateComplete:
				fmt.Fprintln(os.Stdout)
				return nil
			}
		}
	}
}
