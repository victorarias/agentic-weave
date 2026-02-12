package tools

import (
	"time"

	"github.com/victorarias/agentic-weave/agentic"
)

// Options controls built-in tool configuration.
type Options struct {
	WorkDir     string
	BashTimeout time.Duration
	EnableBash  bool
}

// NewBuiltins returns the default coding tools.
func NewBuiltins(opts Options) []agentic.Tool {
	workDir := normalizeWorkDir(opts.WorkDir)
	bashTimeout := opts.BashTimeout
	if bashTimeout <= 0 {
		bashTimeout = 30 * time.Second
	}
	builtins := []agentic.Tool{
		ReadTool{WorkDir: workDir},
		WriteTool{WorkDir: workDir},
		EditTool{WorkDir: workDir},
		GrepTool{WorkDir: workDir},
		GlobTool{WorkDir: workDir},
		LSTool{WorkDir: workDir},
	}
	if opts.EnableBash {
		builtins = append([]agentic.Tool{BashTool{WorkDir: workDir, Timeout: bashTimeout}}, builtins...)
	}
	return builtins
}

// RegisterBuiltins registers all default coding tools.
func RegisterBuiltins(reg *agentic.Registry, opts Options) error {
	builtins := NewBuiltins(opts)
	for _, tool := range builtins {
		if err := reg.Register(tool); err != nil {
			return err
		}
	}
	return nil
}
