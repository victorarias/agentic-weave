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
	return waitNonInteractive(runCtx, sess.Updates(), out)
}

func waitNonInteractive(ctx context.Context, updates <-chan session.Update, out io.Writer) error {
	for {
		// Prefer draining terminal updates before treating context timeout as final.
		select {
		case update, ok := <-updates:
			if !ok {
				return errors.New("non-interactive: updates channel closed")
			}
			done, err := handleNonInteractiveUpdate(update, out)
			if done {
				return err
			}
			continue
		default:
		}

		select {
		case update, ok := <-updates:
			if !ok {
				return errors.New("non-interactive: updates channel closed")
			}
			done, err := handleNonInteractiveUpdate(update, out)
			if done {
				return err
			}
		case <-ctx.Done():
			// Drain one last update if it is already available before returning timeout.
			select {
			case update, ok := <-updates:
				if !ok {
					return errors.New("non-interactive: updates channel closed")
				}
				done, err := handleNonInteractiveUpdate(update, out)
				if done {
					return err
				}
			default:
			}
			return ctx.Err()
		}
	}
}

func handleNonInteractiveUpdate(update session.Update, out io.Writer) (bool, error) {
	switch update.Type {
	case session.UpdateRunEnd:
		reply := ""
		if update.Result != nil {
			reply = strings.TrimSpace(sanitize.Text(update.Result.Reply))
		}
		if reply == "" {
			return true, errors.New("non-interactive: assistant returned empty reply")
		}
		if _, err := fmt.Fprintln(out, reply); err != nil {
			return true, err
		}
		return true, nil
	case session.UpdateRunError:
		if update.Err != nil {
			return true, update.Err
		}
		return true, errors.New("non-interactive: run failed")
	default:
		return false, nil
	}
}
