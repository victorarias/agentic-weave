package relay

import (
	"context"
	"errors"
	"io"
	"log"
	"os/exec"
	"sync"

	"github.com/victorarias/agentic-weave/remotecontrol/runtime"
)

type LaunchRequest struct {
	SessionID              string
	PersistedSessionHandle string
	RelayURL               string
	Token                  string
	PiBin                  string
	RuntimeDescriptor      runtime.Descriptor
}

type Launcher interface {
	Spawn(ctx context.Context, req LaunchRequest) error
	Stop(sessionID string) error
}

var errRuntimeAlreadyManaged = errors.New("runtime already managed for session")

type ProcessLauncher struct {
	WrapperBin string
	Logger     *log.Logger

	mu        sync.Mutex
	processes map[string]*exec.Cmd
}

func NewProcessLauncher(wrapperBin string, logger *log.Logger) *ProcessLauncher {
	if wrapperBin == "" {
		wrapperBin = "weave-wrapper"
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &ProcessLauncher{
		WrapperBin: wrapperBin,
		Logger:     logger,
		processes:  make(map[string]*exec.Cmd),
	}
}

func (l *ProcessLauncher) Spawn(ctx context.Context, req LaunchRequest) error {
	l.mu.Lock()
	if existing := l.processes[req.SessionID]; existing != nil && existing.Process != nil {
		l.mu.Unlock()
		return errRuntimeAlreadyManaged
	}
	l.mu.Unlock()

	args := []string{
		"--relay", req.RelayURL,
		"--token", req.Token,
		"--session", req.SessionID,
	}
	if req.PiBin != "" {
		args = append(args, "--pi-bin", req.PiBin)
	}
	if req.PersistedSessionHandle != "" {
		args = append(args, "--pi-arg=--session", "--pi-arg="+req.PersistedSessionHandle)
	}

	cmd := exec.CommandContext(ctx, l.WrapperBin, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}

	l.mu.Lock()
	l.processes[req.SessionID] = cmd
	l.mu.Unlock()

	go func(sessionID string, cmd *exec.Cmd) {
		_ = cmd.Wait()
		l.mu.Lock()
		if l.processes[sessionID] == cmd {
			delete(l.processes, sessionID)
		}
		l.mu.Unlock()
	}(req.SessionID, cmd)

	return nil
}

func (l *ProcessLauncher) Stop(sessionID string) error {
	l.mu.Lock()
	cmd := l.processes[sessionID]
	if cmd != nil {
		delete(l.processes, sessionID)
	}
	l.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return exec.ErrNotFound
	}
	return cmd.Process.Kill()
}
