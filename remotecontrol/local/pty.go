package local

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

func (w *Wrapper) startPTYRuntime(ctx context.Context) (<-chan error, func(), error) {
	cmd := exec.CommandContext(ctx, w.cfg.PTYBin, w.cfg.PTYArgs...)
	if len(w.cfg.Env) > 0 {
		env := os.Environ()
		for key, value := range w.cfg.Env {
			env = append(env, key+"="+value)
		}
		cmd.Env = env
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(w.cfg.PTYRows), Cols: uint16(w.cfg.PTYCols)})
	if err != nil {
		return nil, nil, err
	}
	w.ptyMu.Lock()
	w.ptyFile = ptmx
	w.ptyRows = w.cfg.PTYRows
	w.ptyCols = w.cfg.PTYCols
	w.ptyMu.Unlock()

	cleanup := func() {
		w.ptyMu.Lock()
		file := w.ptyFile
		w.ptyFile = nil
		w.ptyMu.Unlock()
		if file != nil {
			_ = file.Close()
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}

	procErr := make(chan error, 1)
	go func() {
		procErr <- cmd.Wait()
	}()
	go w.readPTYOutput(ptmx)
	w.bootstrapState = map[string]any{}
	return procErr, cleanup, nil
}

func (w *Wrapper) readPTYOutput(ptmx *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(buf[:n])
			event := protocol.PTYOutputEvent{Event: protocol.EventPTYOutput, Data: encoded}
			env, envErr := protocol.NewEnvelope(protocol.MessageEvent, w.cfg.SessionID, w.runtimeID, "weave-wrapper", "", event)
			if envErr == nil {
				for _, p := range w.snapshotInitializedPeers() {
					_ = p.writeEnvelope(env)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			w.broadcastUpdate(protocol.SessionUpdate{Kind: protocol.UpdateError, Message: "pty read failed", Details: map[string]any{"error": err.Error()}})
			return
		}
	}
}

func (w *Wrapper) writePTYInput(data string) error {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("invalid pty input data: %w", err)
	}
	w.ptyMu.Lock()
	defer w.ptyMu.Unlock()
	if w.ptyFile == nil {
		return fmt.Errorf("pty is not active")
	}
	_, err = w.ptyFile.Write(decoded)
	return err
}

func (w *Wrapper) resizePTY(rows, cols int) error {
	if rows <= 0 || cols <= 0 {
		return fmt.Errorf("invalid pty size rows=%d cols=%d", rows, cols)
	}
	w.ptyMu.Lock()
	if w.ptyFile == nil {
		w.ptyMu.Unlock()
		return fmt.Errorf("pty is not active")
	}
	if err := pty.Setsize(w.ptyFile, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}); err != nil {
		w.ptyMu.Unlock()
		return err
	}
	w.ptyRows = rows
	w.ptyCols = cols
	w.ptyMu.Unlock()
	w.broadcastUpdate(protocol.SessionUpdate{Kind: protocol.UpdateStatus, Phase: w.phase, Details: map[string]any{"pty_rows": rows, "pty_cols": cols, "pty_resize_at": time.Now().UTC().Format(time.RFC3339Nano)}})
	return nil
}
