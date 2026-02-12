package persist

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/victorarias/agentic-weave/agentic/message"
)

func TestStoreAppendLoadReplace(t *testing.T) {
	workDir := t.TempDir()
	store, err := NewStore(workDir, "s1")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	msgA := message.AgentMessage{Role: message.RoleUser, Content: "hello", Timestamp: time.Now()}
	msgB := message.AgentMessage{Role: message.RoleAssistant, Content: "world", Timestamp: time.Now()}
	if err := store.Append(context.Background(), msgA); err != nil {
		t.Fatalf("append A: %v", err)
	}
	if err := store.Append(context.Background(), msgB); err != nil {
		t.Fatalf("append B: %v", err)
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 2 || loaded[0].Content != "hello" || loaded[1].Content != "world" {
		t.Fatalf("unexpected loaded messages: %#v", loaded)
	}

	if err := store.Replace(context.Background(), []message.AgentMessage{msgB}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	loaded, err = store.Load(context.Background())
	if err != nil {
		t.Fatalf("load after replace: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Content != "world" {
		t.Fatalf("unexpected replaced messages: %#v", loaded)
	}
}

func TestStorePathAndDefaultSessionID(t *testing.T) {
	workDir := t.TempDir()
	store, err := NewStore(workDir, "")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	want := filepath.Join(workDir, ".wv", "sessions", "default.json")
	if got := store.Path(); got != want {
		t.Fatalf("unexpected store path got=%q want=%q", got, want)
	}
}

func TestStoreRejectsInvalidSessionID(t *testing.T) {
	if _, err := NewStore(t.TempDir(), "../bad"); err == nil {
		t.Fatal("expected invalid session id error")
	}
}
