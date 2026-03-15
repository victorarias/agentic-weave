package ptytest

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

type Options struct {
	Dir         string
	Env         []string
	Rows        uint16
	Cols        uint16
	ArtifactDir string
	Metadata    map[string]any
}

type Session struct {
	cmd         *exec.Cmd
	pty         *os.File
	artifactDir string
	meta        map[string]any

	mu      sync.Mutex
	stdout  bytes.Buffer
	stdin   bytes.Buffer
	readErr error
	doneCh  chan struct{}
}

func Start(command string, args []string, opts Options) (*Session, error) {
	cmd := exec.Command(command, args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}
	sz := &pty.Winsize{Rows: opts.Rows, Cols: opts.Cols}
	if sz.Rows == 0 {
		sz.Rows = 40
	}
	if sz.Cols == 0 {
		sz.Cols = 120
	}
	ptmx, err := pty.StartWithSize(cmd, sz)
	if err != nil {
		return nil, err
	}
	s := &Session{
		cmd:         cmd,
		pty:         ptmx,
		artifactDir: opts.ArtifactDir,
		meta:        cloneMap(opts.Metadata),
		doneCh:      make(chan struct{}),
	}
	go s.captureOutput()
	return s, nil
}

func (s *Session) captureOutput() {
	defer close(s.doneCh)
	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.mu.Lock()
			_, _ = s.stdout.Write(buf[:n])
			s.mu.Unlock()
		}
		if err != nil {
			s.mu.Lock()
			s.readErr = err
			s.mu.Unlock()
			return
		}
	}
}

func (s *Session) Send(data []byte) error {
	s.mu.Lock()
	_, _ = s.stdin.Write(data)
	s.mu.Unlock()
	_, err := s.pty.Write(data)
	return err
}

func (s *Session) SendString(v string) error { return s.Send([]byte(v)) }

func (s *Session) SendChunked(data []byte, delay time.Duration) error {
	for _, b := range data {
		if err := s.Send([]byte{b}); err != nil {
			return err
		}
		if delay > 0 {
			time.Sleep(delay)
		}
	}
	return nil
}

func (s *Session) Resize(rows, cols uint16) error {
	return pty.Setsize(s.pty, &pty.Winsize{Rows: rows, Cols: cols})
}

func (s *Session) WaitFor(substr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(s.Snapshot(), substr) {
			return nil
		}
		select {
		case <-s.doneCh:
			if strings.Contains(s.Snapshot(), substr) {
				return nil
			}
			return fmt.Errorf("missing %q before process exited", substr)
		case <-time.After(25 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for %q; tail=%s", substr, tailString(s.Snapshot(), 2000))
}

func (s *Session) WaitExit(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if processDone(s.cmd) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("process did not exit within %s", timeout)
}

func processDone(cmd *exec.Cmd) bool {
	if cmd == nil {
		return true
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return true
	}
	return false
}

func (s *Session) Snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return StripANSI(s.stdout.Bytes())
}

func (s *Session) RawOutput() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.stdout.Bytes()...)
}

func (s *Session) RawInput() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.stdin.Bytes()...)
}

func (s *Session) Close() error {
	if s.pty != nil {
		_ = s.pty.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
	<-s.doneCh
	return s.WriteArtifacts(nil)
}

func (s *Session) Interrupt() error {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Signal(os.Interrupt)
	}
	return nil
}

func (s *Session) WriteArtifacts(extra map[string]any) error {
	if s.artifactDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.artifactDir, 0o755); err != nil {
		return err
	}
	meta := cloneMap(s.meta)
	for k, v := range extra {
		meta[k] = v
	}
	meta["command"] = commandLine(s.cmd)
	meta["captured_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	meta["stdout_bytes"] = len(s.RawOutput())
	meta["stdin_bytes"] = len(s.RawInput())
	if err := os.WriteFile(filepath.Join(s.artifactDir, "stdout.bin"), s.RawOutput(), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.artifactDir, "stdout.txt"), []byte(s.Snapshot()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.artifactDir, "stdin.bin"), s.RawInput(), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.artifactDir, "stdin.hex.txt"), []byte(hex.Dump(s.RawInput())), 0o644); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.artifactDir, "metadata.json"), data, 0o644)
}

func commandLine(cmd *exec.Cmd) string {
	if cmd == nil {
		return ""
	}
	parts := append([]string{cmd.Path}, cmd.Args[1:]...)
	return strings.Join(parts, " ")
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\].*?(\x07|\x1b\\)|\x1b[@-_]`)

func StripANSI(data []byte) string {
	clean := ansiRE.ReplaceAll(data, nil)
	text := strings.ReplaceAll(string(clean), "\r", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.TrimRight(line, " \t"))
	}
	return strings.Join(out, "\n")
}

func tailString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}
