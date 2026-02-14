package persist

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestStoreConcurrentAppendAcrossInstances(t *testing.T) {
	workDir := t.TempDir()
	storeA, err := NewStore(workDir, "shared")
	if err != nil {
		t.Fatalf("new store A: %v", err)
	}
	storeB, err := NewStore(workDir, "shared")
	if err != nil {
		t.Fatalf("new store B: %v", err)
	}

	const perWriter = 40
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < perWriter; i++ {
			msg := message.AgentMessage{Role: message.RoleUser, Content: "a", Timestamp: time.Now()}
			if err := storeA.Append(context.Background(), msg); err != nil {
				t.Errorf("append A[%d]: %v", i, err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < perWriter; i++ {
			msg := message.AgentMessage{Role: message.RoleAssistant, Content: "b", Timestamp: time.Now()}
			if err := storeB.Append(context.Background(), msg); err != nil {
				t.Errorf("append B[%d]: %v", i, err)
				return
			}
		}
	}()
	close(start)
	wg.Wait()

	loaded, err := storeA.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, want := len(loaded), 2*perWriter; got != want {
		t.Fatalf("unexpected message count: got=%d want=%d", got, want)
	}
}

func TestReleaseLockDoesNotDeleteAnotherOwner(t *testing.T) {
	store, err := NewStore(t.TempDir(), "shared")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	unlock, err := store.acquireLock(context.Background())
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if err := os.WriteFile(store.lockPath, []byte("other-owner\n"), 0o600); err != nil {
		t.Fatalf("rewrite lock: %v", err)
	}
	unlock()
	data, err := os.ReadFile(store.lockPath)
	if err != nil {
		t.Fatalf("expected lock to remain, read error: %v", err)
	}
	if strings.TrimSpace(string(data)) != "other-owner" {
		t.Fatalf("unexpected lock contents: %q", string(data))
	}
}

func TestAcquireLockRespectsContext(t *testing.T) {
	store, err := NewStore(t.TempDir(), "shared")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	unlock, err := store.acquireLock(context.Background())
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = store.acquireLock(ctx)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected deadline/canceled, got %v", err)
	}
}

func TestStaleLockToken(t *testing.T) {
	store, err := NewStore(t.TempDir(), "shared")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(store.lockPath, []byte("token-1\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(store.lockPath, now, now); err != nil {
		t.Fatalf("chtimes fresh: %v", err)
	}

	stale, token, err := store.staleLockToken()
	if err != nil {
		t.Fatalf("staleLockToken fresh: %v", err)
	}
	if stale || token != "" {
		t.Fatalf("expected fresh lock state, got stale=%v token=%q", stale, token)
	}

	past := now.Add(-staleLockAge - time.Second)
	if err := os.Chtimes(store.lockPath, past, past); err != nil {
		t.Fatalf("chtimes stale: %v", err)
	}
	stale, token, err = store.staleLockToken()
	if err != nil {
		t.Fatalf("staleLockToken stale: %v", err)
	}
	if !stale || token != "token-1" {
		t.Fatalf("expected stale lock with token, got stale=%v token=%q", stale, token)
	}
}

func TestBreakStaleLockIfTokenMatches(t *testing.T) {
	store, err := NewStore(t.TempDir(), "shared")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(store.lockPath, []byte("token-1\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	now := time.Now()
	if err := os.Chtimes(store.lockPath, now, now); err != nil {
		t.Fatalf("chtimes fresh: %v", err)
	}
	store.breakStaleLockIfTokenMatches("token-1")
	if _, err := os.Stat(store.lockPath); err != nil {
		t.Fatalf("expected fresh lock to remain on match: %v", err)
	}

	past := now.Add(-staleLockAge - time.Second)
	if err := os.Chtimes(store.lockPath, past, past); err != nil {
		t.Fatalf("chtimes stale: %v", err)
	}
	store.breakStaleLockIfTokenMatches("token-2")
	if _, err := os.Stat(store.lockPath); err != nil {
		t.Fatalf("expected lock to remain on mismatch: %v", err)
	}
	store.breakStaleLockIfTokenMatches("token-1")
	if _, err := os.Stat(store.lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock removed on match, got err=%v", err)
	}
}

func TestBreakStaleLockIfTokenMatchesRemovesEmptyToken(t *testing.T) {
	store, err := NewStore(t.TempDir(), "shared")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(store.lockPath, nil, 0o600); err != nil {
		t.Fatalf("write empty lock: %v", err)
	}
	past := time.Now().Add(-staleLockAge - time.Second)
	if err := os.Chtimes(store.lockPath, past, past); err != nil {
		t.Fatalf("chtimes stale: %v", err)
	}
	store.breakStaleLockIfTokenMatches("")
	if _, err := os.Stat(store.lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected empty stale lock removed, got err=%v", err)
	}
}

func TestReleaseLock(t *testing.T) {
	store, err := NewStore(t.TempDir(), "shared")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(store.lockPath, []byte("token-1\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	store.releaseLock("token-2")
	if _, err := os.Stat(store.lockPath); err != nil {
		t.Fatalf("expected lock to remain on mismatch: %v", err)
	}
	store.releaseLock("token-1")
	if _, err := os.Stat(store.lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock removed on match, got err=%v", err)
	}
}

func TestStoreRejectsUnsupportedVersion(t *testing.T) {
	workDir := t.TempDir()
	store, err := NewStore(workDir, "s1")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload := map[string]any{
		"version":  999,
		"messages": []map[string]any{},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(store.path, data, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if _, err := store.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
}

func TestTouchLockRefreshesStaleLockForOwner(t *testing.T) {
	store, err := NewStore(t.TempDir(), "shared")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(store.lockPath, []byte("token-1\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	past := time.Now().Add(-staleLockAge - time.Second)
	if err := os.Chtimes(store.lockPath, past, past); err != nil {
		t.Fatalf("chtimes stale: %v", err)
	}
	stale, _, err := store.staleLockToken()
	if err != nil {
		t.Fatalf("staleLockToken before touch: %v", err)
	}
	if !stale {
		t.Fatal("expected stale lock before touch")
	}
	store.touchLock("token-1")
	stale, _, err = store.staleLockToken()
	if err != nil {
		t.Fatalf("staleLockToken after touch: %v", err)
	}
	if stale {
		t.Fatal("expected refreshed lock to be non-stale")
	}
}

func TestTouchLockIgnoresNonOwnerToken(t *testing.T) {
	store, err := NewStore(t.TempDir(), "shared")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(store.lockPath, []byte("token-1\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	past := time.Now().Add(-staleLockAge - time.Second)
	if err := os.Chtimes(store.lockPath, past, past); err != nil {
		t.Fatalf("chtimes stale: %v", err)
	}
	store.touchLock("token-2")
	stale, token, err := store.staleLockToken()
	if err != nil {
		t.Fatalf("staleLockToken after wrong touch: %v", err)
	}
	if !stale || token != "token-1" {
		t.Fatalf("expected stale lock unchanged, got stale=%v token=%q", stale, token)
	}
}
