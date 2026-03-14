package relay

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProcessLauncherSpawnKeepsDuplicateGuardAtomic(t *testing.T) {
	tempDir := t.TempDir()
	countFile := filepath.Join(tempDir, "wrapper-starts.log")
	wrapperPath := filepath.Join(tempDir, "fake-wrapper.sh")
	script := "#!/bin/sh\n" +
		"echo started >> \"$WEAVE_LAUNCH_COUNT_FILE\"\n" +
		"trap 'exit 0' TERM INT\n" +
		"while true; do sleep 1; done\n"
	if err := os.WriteFile(wrapperPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write wrapper script: %v", err)
	}
	t.Setenv("WEAVE_LAUNCH_COUNT_FILE", countFile)

	launcher := NewProcessLauncher(wrapperPath, log.New(io.Discard, "", 0))
	originalStart := processLauncherStart
	defer func() { processLauncherStart = originalStart }()

	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	processLauncherStart = func(cmd *exec.Cmd) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return originalStart(cmd)
	}

	req := LaunchRequest{
		SessionID: "sess-atomic",
		RelayURL:  "ws://relay.example/ws",
		Token:     "secret",
	}
	errCh := make(chan error, 2)
	go func() { errCh <- launcher.Spawn(context.Background(), req) }()
	<-entered
	go func() { errCh <- launcher.Spawn(context.Background(), req) }()
	close(release)

	err1 := <-errCh
	err2 := <-errCh
	if err1 != nil && err2 != nil {
		t.Fatalf("expected one successful spawn, got errors %v and %v", err1, err2)
	}
	if err1 == nil && err2 == nil {
		t.Fatal("expected duplicate spawn to be rejected")
	}
	if err1 != nil && !errors.Is(err1, errRuntimeAlreadyManaged) {
		t.Fatalf("expected errRuntimeAlreadyManaged, got %v", err1)
	}
	if err2 != nil && !errors.Is(err2, errRuntimeAlreadyManaged) {
		t.Fatalf("expected errRuntimeAlreadyManaged, got %v", err2)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(countFile)
		if err == nil {
			starts := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(starts) != 1 || starts[0] != "started" {
				t.Fatalf("expected exactly one wrapper start, got %q", string(data))
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read count file: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for wrapper start")
		}
		time.Sleep(25 * time.Millisecond)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := launcher.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown launcher: %v", err)
	}
}
