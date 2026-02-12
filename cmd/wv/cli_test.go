package main

import (
	"strings"
	"testing"
)

func TestParseCLIArgsDefaults(t *testing.T) {
	opts, err := parseCLIArgs(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.SessionID != "default" || opts.NonInteractive || opts.NewSession {
		t.Fatalf("unexpected defaults: %#v", opts)
	}
}

func TestParseCLIArgsNonInteractiveWithPositionalMessage(t *testing.T) {
	opts, err := parseCLIArgs([]string{"--non-interactive", "hello", "world"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.NonInteractive || opts.Message != "hello world" {
		t.Fatalf("unexpected opts: %#v", opts)
	}
}

func TestResolveNonInteractiveMessage(t *testing.T) {
	msg, err := resolveNonInteractiveMessage("", strings.NewReader("  hi  "), false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if msg != "hi" {
		t.Fatalf("expected trimmed piped input, got %q", msg)
	}
}

func TestResolveNonInteractiveMessageErrorsOnEmptyTTY(t *testing.T) {
	if _, err := resolveNonInteractiveMessage("", strings.NewReader(""), true); err == nil {
		t.Fatal("expected error for missing message in tty mode")
	}
}

func TestParseCLIArgsRejectsPositionalInInteractiveMode(t *testing.T) {
	if _, err := parseCLIArgs([]string{"hello"}); err == nil {
		t.Fatal("expected positional argument error in interactive mode")
	}
}

func TestParseCLIArgsRejectsMessageWithoutNonInteractive(t *testing.T) {
	if _, err := parseCLIArgs([]string{"--message", "hello"}); err == nil {
		t.Fatal("expected --message validation error")
	}
}

func TestParseCLIArgsRejectsMixedMessageInputs(t *testing.T) {
	if _, err := parseCLIArgs([]string{"--non-interactive", "--message", "hello", "world"}); err == nil {
		t.Fatal("expected mixed input validation error")
	}
}
