package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/victorarias/agentic-weave/remotecontrol/relay"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "weave-relay:", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("weave-relay", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "Address to listen on")
	token := fs.String("token", "", "Shared bearer token for clients and wrappers")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server := relay.NewServer(relay.Config{
		Addr:   *addr,
		Token:  *token,
		Logger: log.New(os.Stderr, "weave-relay: ", log.LstdFlags),
	})
	return server.Run(ctx)
}
