package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type cliOptions struct {
	NonInteractive bool
	Message        string
	SessionID      string
	NewSession     bool
}

func parseCLIArgs(args []string) (cliOptions, error) {
	opts := cliOptions{SessionID: "default"}
	fs := flag.NewFlagSet("wv", flag.ContinueOnError)
	var out bytes.Buffer
	fs.SetOutput(&out)

	fs.BoolVar(&opts.NonInteractive, "non-interactive", false, "Run once with --message (or piped stdin) and exit.")
	fs.BoolVar(&opts.NonInteractive, "n", false, "Shorthand for --non-interactive.")
	fs.StringVar(&opts.Message, "message", "", "Message for non-interactive mode.")
	fs.StringVar(&opts.Message, "m", "", "Shorthand for --message.")
	fs.StringVar(&opts.SessionID, "session", "default", "Session id for persistence.")
	fs.StringVar(&opts.SessionID, "s", "default", "Shorthand for --session.")
	fs.BoolVar(&opts.NewSession, "new-session", false, "Start with an empty persisted session.")

	if err := fs.Parse(args); err != nil {
		return cliOptions{}, fmt.Errorf("%w\n%s", err, out.String())
	}
	if opts.NonInteractive && strings.TrimSpace(opts.Message) == "" && fs.NArg() > 0 {
		opts.Message = strings.Join(fs.Args(), " ")
	}
	opts.SessionID = strings.TrimSpace(opts.SessionID)
	if opts.SessionID == "" {
		opts.SessionID = "default"
	}
	return opts, nil
}

func resolveNonInteractiveMessage(explicit string, in io.Reader, stdinTTY bool) (string, error) {
	msg := strings.TrimSpace(explicit)
	if msg != "" {
		return msg, nil
	}
	if stdinTTY {
		return "", fmt.Errorf("non-interactive mode requires --message or piped stdin")
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return "", err
	}
	msg = strings.TrimSpace(string(data))
	if msg == "" {
		return "", fmt.Errorf("non-interactive mode received empty input")
	}
	return msg, nil
}

func stdinIsTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return true
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
