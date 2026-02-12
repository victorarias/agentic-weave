package persist

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/victorarias/agentic-weave/agentic/message"
)

var sessionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

const (
	defaultSessionID = "default"
	lockRetryDelay   = 20 * time.Millisecond
	staleLockAge     = 2 * time.Minute
)

type filePayload struct {
	Version  int                    `json:"version"`
	Messages []message.AgentMessage `json:"messages"`
}

// Store persists session history on disk.
type Store struct {
	path     string
	lockPath string
	mu       sync.Mutex
}

// NewStore creates a file-backed session store in .wv/sessions.
func NewStore(workDir, sessionID string) (*Store, error) {
	root := strings.TrimSpace(workDir)
	if root == "" {
		return nil, fmt.Errorf("persist: workdir is required")
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		id = defaultSessionID
	}
	if !sessionIDPattern.MatchString(id) {
		return nil, fmt.Errorf("persist: invalid session id %q", sessionID)
	}
	dir := filepath.Join(root, ".wv", "sessions")
	return &Store{
		path:     filepath.Join(dir, id+".json"),
		lockPath: filepath.Join(dir, id+".lock"),
	}, nil
}

// Path returns the underlying JSON file path.
func (s *Store) Path() string {
	return s.path
}

// Append stores a message.
func (s *Store) Append(ctx context.Context, msg message.AgentMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.acquireLock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	messages, err := s.loadLocked()
	if err != nil {
		return err
	}
	messages = append(messages, msg)
	return s.saveLocked(messages)
}

// Load reads all stored messages.
func (s *Store) Load(ctx context.Context) ([]message.AgentMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.acquireLock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.loadLocked()
}

// Replace overwrites stored messages.
func (s *Store) Replace(ctx context.Context, messages []message.AgentMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.acquireLock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return s.saveLocked(messages)
}

func (s *Store) loadLocked() ([]message.AgentMessage, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var payload filePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("persist: decode %s: %w", s.path, err)
	}
	out := make([]message.AgentMessage, len(payload.Messages))
	copy(out, payload.Messages)
	return out, nil
}

func (s *Store) saveLocked(messages []message.AgentMessage) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	payload := filePayload{
		Version:  1,
		Messages: make([]message.AgentMessage, len(messages)),
	}
	copy(payload.Messages, messages)
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmp)
	}()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err == nil {
		return nil
	}
	// Windows does not always allow rename-over-existing semantics.
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) acquireLock(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(s.lockPath), 0o755); err != nil {
		return nil, err
	}
	for {
		lock, err := os.OpenFile(s.lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(lock, "pid=%d\n", os.Getpid())
			_ = lock.Close()
			return func() {
				_ = os.Remove(s.lockPath)
			}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		stale, staleErr := s.isLockStale()
		if staleErr == nil && stale {
			_ = os.Remove(s.lockPath)
			continue
		}
		timer := time.NewTimer(lockRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Store) isLockStale() (bool, error) {
	info, err := os.Stat(s.lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return time.Since(info.ModTime()) > staleLockAge, nil
}
