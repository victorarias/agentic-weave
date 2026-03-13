package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "weave-inspect:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 || os.Args[1] != "local" {
		return fmt.Errorf("usage: weave-inspect local --socket <path> <init|prompt|cancel> [message]")
	}
	fs := flag.NewFlagSet("weave-inspect local", flag.ContinueOnError)
	socket := fs.String("socket", "/tmp/weave-local.sock", "Unix socket path")
	jsonMode := fs.Bool("json", false, "Print raw JSON envelopes")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	args := fs.Args()
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand: init, prompt, or cancel")
	}
	subcmd := args[0]
	message := ""
	if subcmd == "prompt" {
		if len(args) < 2 {
			return fmt.Errorf("prompt requires a message")
		}
		message = strings.Join(args[1:], " ")
	}

	conn, err := net.Dial("unix", *socket)
	if err != nil {
		return err
	}
	defer conn.Close()

	events := make(chan protocol.Envelope, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- protocol.ReadJSONL(conn, func(line []byte) error {
			env, err := protocol.DecodeEnvelope(line)
			if err != nil {
				return err
			}
			events <- env
			return nil
		})
	}()

	initEnv, err := protocol.NewEnvelope(protocol.MessageCommand, "local", "", "weave-inspect", "init-1", protocol.InitializeCommand{
		Command:         protocol.CommandInitialize,
		ProtocolVersion: protocol.Version,
	})
	if err != nil {
		return err
	}
	if err := protocol.WriteJSONLine(conn, initEnv); err != nil {
		return err
	}
	if err := waitForInit(events, *jsonMode); err != nil {
		return err
	}

	switch subcmd {
	case "init":
		return nil
	case "cancel":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, "local", "", "weave-inspect", "cancel-1", protocol.SessionCancelCommand{Command: protocol.CommandSessionCancel})
		if err != nil {
			return err
		}
		if err := protocol.WriteJSONLine(conn, env); err != nil {
			return err
		}
		return waitForAckOrError(events, "cancel-1", *jsonMode)
	case "prompt":
		env, err := protocol.NewEnvelope(protocol.MessageCommand, "local", "", "weave-inspect", "prompt-1", protocol.SessionPromptCommand{Command: protocol.CommandSessionPrompt, Message: message})
		if err != nil {
			return err
		}
		if err := protocol.WriteJSONLine(conn, env); err != nil {
			return err
		}
		if err := waitForAckOrError(events, "prompt-1", *jsonMode); err != nil {
			return err
		}
		return streamUntilComplete(events, errCh, *jsonMode)
	default:
		return fmt.Errorf("unknown subcommand %q", subcmd)
	}
}

func waitForInit(events <-chan protocol.Envelope, jsonMode bool) error {
	deadline := time.After(10 * time.Second)
	seenAck := false
	seenReady := false
	for !(seenAck && seenReady) {
		select {
		case env := <-events:
			if jsonMode {
				_ = protocol.WriteJSONLine(os.Stdout, env)
			}
			if env.Type == protocol.MessageAck && env.ID == "init-1" {
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
