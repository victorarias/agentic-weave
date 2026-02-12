package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/victorarias/agentic-weave/cmd/wv/sanitize"
	"github.com/victorarias/agentic-weave/cmd/wv/session"
)

func runNonInteractive(ctx context.Context, sess *session.Session, prompt string, timeout time.Duration, out io.Writer) error {
	if sess == nil {
		return errors.New("non-interactive: session is required")
	}
	message := strings.TrimSpace(sanitize.Text(prompt))
	if message == "" {
		return errors.New("non-interactive: message is required")
	}
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := sess.Send(runCtx, message); err != nil {
		return err
	}

	for {
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case update := <-sess.Updates():
			switch update.Type {
			case session.UpdateRunEnd:
				reply := ""
				if update.Result != nil {
					reply = strings.TrimSpace(sanitize.Text(update.Result.Reply))
				}
				if reply != "" {
					_, _ = fmt.Fprintln(out, reply)
				}
				return nil
			case session.UpdateRunError:
				if update.Err != nil {
					return update.Err
				}
				return errors.New("non-interactive: run failed")
			}
		}
	}
}
