package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/cmd/wv/session"
)

type niStaticDecider struct {
	reply string
}

func (d niStaticDecider) Decide(_ context.Context, _ loop.Input) (loop.Decision, error) {
	return loop.Decision{Reply: d.reply}, nil
}

type niBlockingDecider struct{}

func (niBlockingDecider) Decide(ctx context.Context, _ loop.Input) (loop.Decision, error) {
	<-ctx.Done()
	return loop.Decision{}, ctx.Err()
}

func TestRunNonInteractivePrintsReply(t *testing.T) {
	s, err := session.New(session.Config{Decider: niStaticDecider{reply: "hi"}})
	if err != nil {
		t.Fatalf("session new: %v", err)
	}

	var out bytes.Buffer
	if err := runNonInteractive(context.Background(), s, "hello", time.Second, &out); err != nil {
		t.Fatalf("run non-interactive: %v", err)
	}
	if strings.TrimSpace(out.String()) != "hi" {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestRunNonInteractiveTimeout(t *testing.T) {
	s, err := session.New(session.Config{Decider: niBlockingDecider{}})
	if err != nil {
		t.Fatalf("session new: %v", err)
	}
	var out bytes.Buffer
	err = runNonInteractive(context.Background(), s, "hello", 20*time.Millisecond, &out)
	if err == nil {
		t.Fatal("expected timeout/cancellation error")
	}
}
