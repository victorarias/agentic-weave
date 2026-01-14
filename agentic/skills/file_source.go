package skills

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileSource loads markdown skills from a directory.
type FileSource struct {
	Root      string
	Allowlist map[string]struct{}
}

func (f FileSource) List(ctx context.Context) ([]Skill, error) {
	root := f.Root
	if root == "" {
		return nil, fmt.Errorf("skills root is empty")
	}
	var skillsList []Skill
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		if len(f.Allowlist) > 0 {
			if _, ok := f.Allowlist[name]; !ok {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		skill, err := loadSkillFile(path, name)
		if err != nil {
			return err
		}
		skillsList = append(skillsList, skill)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return skillsList, nil
}

func loadSkillFile(path, fallbackName string) (Skill, error) {
	file, err := os.Open(path)
	if err != nil {
		return Skill{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var (
		frontmatter map[string]string
		lines       []string
		inFront     bool
		doneFront   bool
	)
	for scanner.Scan() {
		line := scanner.Text()
		if !doneFront && strings.TrimSpace(line) == "---" {
			if !inFront {
				inFront = true
				frontmatter = make(map[string]string)
				continue
			}
			doneFront = true
			continue
		}
		if inFront && !doneFront {
			if key, value, ok := parseFrontmatterLine(line); ok {
				frontmatter[key] = value
			}
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return Skill{}, err
	}

	skill := Skill{
		ID:     fallbackName,
		Name:   fallbackName,
		Body:   strings.TrimSpace(strings.Join(lines, "\n")),
		Source: path,
	}
	if title, ok := frontmatter["title"]; ok && title != "" {
		skill.Name = title
	}
	if desc, ok := frontmatter["description"]; ok {
		skill.Description = desc
	}
	if tags, ok := frontmatter["tags"]; ok {
		skill.Tags = splitTags(tags)
	}
	return skill, nil
}

func parseFrontmatterLine(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" {
		return "", "", false
	}
	return strings.ToLower(key), value, true
}

func splitTags(value string) []string {
	parts := strings.Split(value, ",")
	var tags []string
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		tags = append(tags, tag)
	}
	return tags
}
