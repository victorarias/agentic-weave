package relay

import (
	"context"
	"errors"
	"io"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"

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

const gracefulStopTimeout = 3 * time.Second

type managedProcess struct {
	cmd  *exec.Cmd
	done chan error
}

type ProcessLauncher struct {
	WrapperBin string
	Logger     *log.Logger

	mu        sync.Mutex
	processes map[string]*managedProcess
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
		processes:  make(map[string]*managedProcess),
	}
}

func (l *ProcessLauncher) Spawn(ctx context.Context, req LaunchRequest) error {
	l.mu.Lock()
	if existing := l.processes[req.SessionID]; existing != nil && existing.cmd != nil && existing.cmd.Process != nil {
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

	proc := &managedProcess{cmd: cmd, done: make(chan error, 1)}
	l.mu.Lock()
	l.processes[req.SessionID] = proc
	l.mu.Unlock()

	go func(sessionID string, proc *managedProcess) {
		err := proc.cmd.Wait()
		proc.done <- err
		l.mu.Lock()
		if l.processes[sessionID] == proc {
			delete(l.processes, sessionID)
		}
		l.mu.Unlock()
	}(req.SessionID, proc)

	return nil
}

func (l *ProcessLauncher) Stop(sessionID string) error {
	l.mu.Lock()
	proc := l.processes[sessionID]
	if proc != nil {
		delete(l.processes, sessionID)
	}
	l.mu.Unlock()
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return exec.ErrNotFound
	}
	if err := proc.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	select {
	case <-proc.done:
		return nil
	case <-time.After(gracefulStopTimeout):
		return proc.cmd.Process.Kill()
	}
}
