package main

import (
	"bytes"
	"context"
	"errors"
	"io"
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

type niEmptyReplyDecider struct{}

func (niEmptyReplyDecider) Decide(_ context.Context, _ loop.Input) (loop.Decision, error) {
	return loop.Decision{Reply: ""}, nil
}

type errWriter struct{}

func (errWriter) Write(_ []byte) (int, error) { return 0, errors.New("write failed") }

func TestRunNonInteractiveErrorsOnEmptyReply(t *testing.T) {
	s, err := session.New(session.Config{Decider: niEmptyReplyDecider{}})
	if err != nil {
		t.Fatalf("session new: %v", err)
	}
	var out bytes.Buffer
	err = runNonInteractive(context.Background(), s, "hello", time.Second, &out)
	if err == nil || !strings.Contains(err.Error(), "empty reply") {
		t.Fatalf("expected empty reply error, got %v", err)
	}
}

func TestRunNonInteractivePropagatesWriterFailure(t *testing.T) {
	s, err := session.New(session.Config{Decider: niStaticDecider{reply: "hi"}})
	if err != nil {
		t.Fatalf("session new: %v", err)
	}
	err = runNonInteractive(context.Background(), s, "hello", time.Second, io.Writer(errWriter{}))
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected writer error, got %v", err)
	}
}

func TestWaitNonInteractiveErrorsWhenUpdatesClose(t *testing.T) {
	updates := make(chan session.Update)
	close(updates)
	var out bytes.Buffer
	err := waitNonInteractive(context.Background(), updates, &out)
	if err == nil || !strings.Contains(err.Error(), "updates channel closed") {
		t.Fatalf("expected closed updates error, got %v", err)
	}
}
