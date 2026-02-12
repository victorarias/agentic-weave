package extensions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoaderLoadsProjectExtensions(t *testing.T) {
	workDir := t.TempDir()
	extDir := filepath.Join(workDir, ".wv", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(extDir, "hello.lua")
	if err := os.WriteFile(file, []byte("local wv = require(\"wv\")\nwv.log(\"loaded \" .. wv.config.working_dir)\n"), 0o644); err != nil {
		t.Fatalf("write lua: %v", err)
	}

	loader := NewLoader(workDir)
	defer loader.Close()
	if err := loader.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	loaded := loader.Loaded()
	if len(loaded) != 1 || loaded[0] != file {
		t.Fatalf("unexpected loaded files: %#v", loaded)
	}
}

func TestLoaderReloadReflectsFileSetChanges(t *testing.T) {
	workDir := t.TempDir()
	extDir := filepath.Join(workDir, ".wv", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	first := filepath.Join(extDir, "first.lua")
	second := filepath.Join(extDir, "second.lua")
	if err := os.WriteFile(first, []byte("local wv = require(\"wv\")\nwv.log(\"first\")\n"), 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}

	loader := NewLoader(workDir)
	defer loader.Close()
	if err := loader.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loader.Loaded()) != 1 {
		t.Fatalf("expected 1 loaded file, got %#v", loader.Loaded())
	}

	if err := os.WriteFile(second, []byte("local wv = require(\"wv\")\nwv.log(\"second\")\n"), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}
	if err := loader.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(loader.Loaded()) != 2 {
		t.Fatalf("expected 2 loaded files after reload, got %#v", loader.Loaded())
	}
}

func TestLoaderDeduplicatesWhenHomeEqualsWorkdir(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv("HOME", workDir)

	extDir := filepath.Join(workDir, ".wv", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(extDir, "single.lua")
	if err := os.WriteFile(file, []byte("local wv = require(\"wv\")\nwv.log(\"once\")\n"), 0o644); err != nil {
		t.Fatalf("write lua: %v", err)
	}

	loader := NewLoader(workDir)
	defer loader.Close()
	if err := loader.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loader.Loaded()) != 1 {
		t.Fatalf("expected deduped load count 1, got %#v", loader.Loaded())
	}
}

func TestLoaderCanDisableProjectExtensions(t *testing.T) {
	workDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".wv", "extensions")
	projectDir := filepath.Join(workDir, ".wv", "extensions")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir global: %v", err)
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	globalFile := filepath.Join(globalDir, "global.lua")
	projectFile := filepath.Join(projectDir, "project.lua")
	if err := os.WriteFile(globalFile, []byte("local wv = require(\"wv\")\nwv.log(\"global\")\n"), 0o644); err != nil {
		t.Fatalf("write global: %v", err)
	}
	if err := os.WriteFile(projectFile, []byte("local wv = require(\"wv\")\nwv.log(\"project\")\n"), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}

	loader := NewLoaderWithOptions(workDir, Options{
		IncludeGlobal:  true,
		IncludeProject: false,
	})
	defer loader.Close()
	if err := loader.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	loaded := loader.Loaded()
	if len(loaded) != 1 || loaded[0] != globalFile {
		t.Fatalf("expected only global extension loaded, got %#v", loaded)
	}
}
