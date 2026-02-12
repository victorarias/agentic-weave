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

	"github.com/victorarias/agentic-weave/agentic/message"
)

var sessionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

type filePayload struct {
	Version  int                    `json:"version"`
	Messages []message.AgentMessage `json:"messages"`
}

// Store persists session history on disk.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore creates a file-backed session store in .wv/sessions.
func NewStore(workDir, sessionID string) (*Store, error) {
	root := strings.TrimSpace(workDir)
	if root == "" {
		return nil, fmt.Errorf("persist: workdir is required")
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		id = "default"
	}
	if !sessionIDPattern.MatchString(id) {
		return nil, fmt.Errorf("persist: invalid session id %q", sessionID)
	}
	dir := filepath.Join(root, ".wv", "sessions")
	return &Store{
		path: filepath.Join(dir, id+".json"),
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
	return s.loadLocked()
}

// Replace overwrites stored messages.
func (s *Store) Replace(ctx context.Context, messages []message.AgentMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
