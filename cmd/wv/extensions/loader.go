package extensions

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Loader discovers and executes Lua extensions.
type Loader struct {
	workDir        string
	includeGlobal  bool
	includeProject bool
	state          *lua.LState
	loaded         []string
}

// Options controls extension discovery locations.
type Options struct {
	IncludeGlobal  bool
	IncludeProject bool
}

// NewLoader creates a loader scoped to a workspace.
func NewLoader(workDir string) *Loader {
	return NewLoaderWithOptions(workDir, Options{
		IncludeGlobal:  true,
		IncludeProject: true,
	})
}

// NewLoaderWithOptions creates a loader scoped to selected discovery roots.
func NewLoaderWithOptions(workDir string, opts Options) *Loader {
	return &Loader{
		workDir:        strings.TrimSpace(workDir),
		includeGlobal:  opts.IncludeGlobal,
		includeProject: opts.IncludeProject,
	}
}

// Load initializes the Lua VM and executes all extension files.
func (l *Loader) Load() error {
	if l.state != nil {
		l.state.Close()
		l.state = nil
	}
	l.loaded = nil

	state := lua.NewState(lua.Options{SkipOpenLibs: false})
	l.preloadAPI(state)

	files, err := l.discover()
	if err != nil {
		state.Close()
		return err
	}
	for _, file := range files {
		if err := state.DoFile(file); err != nil {
			state.Close()
			return fmt.Errorf("extensions: %s: %w", file, err)
		}
		l.loaded = append(l.loaded, file)
	}
	l.state = state
	return nil
}

// Reload re-executes all extension files in a fresh Lua state.
func (l *Loader) Reload() error {
	return l.Load()
}

// Loaded returns the extension files loaded in the latest successful run.
func (l *Loader) Loaded() []string {
	out := make([]string, len(l.loaded))
	copy(out, l.loaded)
	return out
}

// Close tears down the Lua state.
func (l *Loader) Close() {
	if l.state != nil {
		l.state.Close()
		l.state = nil
	}
}

func (l *Loader) discover() ([]string, error) {
	dirs := make([]string, 0, 2)
	if l.includeGlobal {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			dirs = append(dirs, filepath.Join(home, ".wv", "extensions"))
		}
	}
	if l.includeProject && l.workDir != "" {
		dirs = append(dirs, filepath.Join(l.workDir, ".wv", "extensions"))
	}

	seenDirs := make(map[string]struct{}, len(dirs))
	uniqueDirs := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		canonical := filepath.Clean(dir)
		if _, ok := seenDirs[canonical]; ok {
			continue
		}
		seenDirs[canonical] = struct{}{}
		uniqueDirs = append(uniqueDirs, canonical)
	}

	files := make([]string, 0, 16)
	seenFiles := make(map[string]struct{}, 16)
	for _, dir := range uniqueDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if filepath.Ext(entry.Name()) != ".lua" {
				continue
			}
			file := filepath.Join(dir, entry.Name())
			if _, ok := seenFiles[file]; ok {
				continue
			}
			seenFiles[file] = struct{}{}
			files = append(files, file)
		}
	}
	sort.Strings(files)
	return files, nil
}

func (l *Loader) preloadAPI(state *lua.LState) {
	module := state.NewTable()
	config := state.NewTable()
	state.SetField(config, "working_dir", lua.LString(l.workDir))
	state.SetField(module, "config", config)
	state.SetField(module, "loaded_count", lua.LNumber(len(l.loaded)))
	state.SetField(module, "log", state.NewFunction(func(s *lua.LState) int {
		// Keep API stable; current implementation is intentionally no-op.
		_ = s.CheckString(1)
		return 0
	}))

	state.PreloadModule("wv", func(s *lua.LState) int {
		s.Push(module)
		return 1
	})
}
