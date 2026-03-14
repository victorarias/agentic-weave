package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/victorarias/agentic-weave/remotecontrol/local"
)

type multiFlag []string

func (m *multiFlag) String() string { return fmt.Sprintf("%v", []string(*m)) }
func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "weave-wrapper:", err)
		os.Exit(1)
	}
}

func run() error {
	var piArgs multiFlag
	fs := flag.NewFlagSet("weave-wrapper", flag.ContinueOnError)
	socket := fs.String("socket", "/tmp/weave-local.sock", "Unix socket path")
	relayURL := fs.String("relay", "", "Relay websocket URL (e.g. ws://localhost:8080/ws)")
	token := fs.String("token", "", "Shared bearer token for relay mode")
	sessionID := fs.String("session", "local", "Logical session id")
	piBin := fs.String("pi-bin", "pi", "Path to pi binary")
	fs.Var(&piArgs, "pi-arg", "Additional argument passed to pi (repeatable)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	wrapper := local.NewWrapper(local.Config{
		SocketPath: *socket,
		SessionID:  *sessionID,
		PiBin:      *piBin,
		PiArgs:     piArgs,
		Logger:     log.New(os.Stderr, "weave-wrapper: ", log.LstdFlags),
	})
	if *relayURL != "" {
		return wrapper.RunRelay(ctx, *relayURL, *token)
	}
	return wrapper.Run(ctx)
}
