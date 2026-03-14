package relay

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
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
	Shutdown(ctx context.Context) error
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
	_, err := stopManagedProcess(context.Background(), proc, gracefulStopTimeout)
	return err
}

func (l *ProcessLauncher) Shutdown(ctx context.Context) error {
	l.mu.Lock()
	processes := make([]*managedProcess, 0, len(l.processes))
	for sessionID, proc := range l.processes {
		if proc != nil {
			processes = append(processes, proc)
		}
		delete(l.processes, sessionID)
	}
	l.mu.Unlock()

	var firstErr error
	for _, proc := range processes {
		if _, err := stopManagedProcess(ctx, proc, gracefulStopTimeout); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func stopManagedProcess(ctx context.Context, proc *managedProcess, timeout time.Duration) (bool, error) {
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return false, exec.ErrNotFound
	}
	if err := proc.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return false, err
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	select {
	case <-proc.done:
		return false, nil
	case <-ctx.Done():
		if err := proc.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return true, err
		}
		<-proc.done
		return true, ctx.Err()
	case <-deadline.C:
		if err := proc.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return true, err
		}
		<-proc.done
		return true, nil
	}
}
