package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSourceListAllowlist(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.md"), []byte("Alpha"), 0o600); err != nil {
		t.Fatalf("write alpha: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beta.md"), []byte("Beta"), 0o600); err != nil {
		t.Fatalf("write beta: %v", err)
	}

	src := FileSource{Root: dir, Allowlist: map[string]struct{}{"alpha": {}}}
	skills, err := src.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 || skills[0].ID != "alpha" {
		t.Fatalf("expected allowlisted skill")
	}
}

func TestFileSourceFrontmatterInvalidLine(t *testing.T) {
	dir := t.TempDir()
	content := []byte("---\ntitle: Hello\nbadline\n---\nBody")
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	src := FileSource{Root: dir}
	if _, err := src.List(context.Background()); err == nil {
		t.Fatalf("expected frontmatter parse error")
	}
}

func TestFileSourceFrontmatterUnterminated(t *testing.T) {
	dir := t.TempDir()
	content := []byte("---\ntitle: Hello\nBody")
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	src := FileSource{Root: dir}
	if _, err := src.List(context.Background()); err == nil {
		t.Fatalf("expected unterminated frontmatter error")
	}
}

func TestFileSourceFrontmatterFields(t *testing.T) {
	dir := t.TempDir()
	content := []byte("---\ntitle: Custom\ndescription: Example\ntags: one, two\n---\nBody")
	if err := os.WriteFile(filepath.Join(dir, "good.md"), content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	src := FileSource{Root: dir}
	skills, err := src.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill")
	}
	if skills[0].Name != "Custom" || skills[0].Description != "Example" {
		t.Fatalf("expected frontmatter fields to load")
	}
	if len(skills[0].Tags) != 2 {
		t.Fatalf("expected tag parsing")
	}
}
