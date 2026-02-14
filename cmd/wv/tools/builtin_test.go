package tools

import (
	"testing"
	"time"
)

func TestNewBuiltinsWithoutBash(t *testing.T) {
	builtins := NewBuiltins(Options{WorkDir: t.TempDir()})
	if len(builtins) != 6 {
		t.Fatalf("expected 6 builtins without bash, got %d", len(builtins))
	}
	if builtins[0].Definition().Name == "bash" {
		t.Fatal("did not expect bash in default builtin set")
	}
}

func TestNewBuiltinsWithBash(t *testing.T) {
	builtins := NewBuiltins(Options{WorkDir: t.TempDir(), EnableBash: true, BashTimeout: 5 * time.Second})
	if len(builtins) != 7 {
		t.Fatalf("expected 7 builtins with bash, got %d", len(builtins))
	}
	if builtins[0].Definition().Name != "bash" {
		t.Fatalf("expected bash first for visibility, got %q", builtins[0].Definition().Name)
	}
}
