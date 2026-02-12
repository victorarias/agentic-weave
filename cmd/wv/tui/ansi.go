package tui

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// StripANSI removes CSI sequences for width calculations.
func StripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

// VisibleWidth estimates display width for monospace terminals.
func VisibleWidth(value string) int {
	return len([]rune(StripANSI(value)))
}

// TruncateVisible truncates text to width and appends ellipsis when needed.
func TruncateVisible(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if VisibleWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "."
	}
	if width <= 3 {
		runes := []rune(StripANSI(value))
		return string(runes[:width])
	}

	target := width - 3
	var b strings.Builder
	visible := 0
	hasANSI := false

	for i := 0; i < len(value) && visible < target; {
		if value[i] == 0x1b {
			if seq, n, ok := readANSI(value[i:]); ok {
				hasANSI = true
				b.WriteString(seq)
				i += n
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		b.WriteRune(r)
		i += size
		visible++
	}
	b.WriteString("...")
	if hasANSI {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// WrapText wraps text on word boundaries for a given width.
func WrapText(value string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	lines := make([]string, 0)
	for _, paragraph := range strings.Split(value, "\n") {
		paragraph = strings.TrimRight(paragraph, "\r")
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		runes := []rune(paragraph)
		start := 0
		for start < len(runes) {
			end := start
			cols := 0
			lastSpace := -1
			for end < len(runes) && cols < width {
				if runes[end] == ' ' || runes[end] == '\t' {
					lastSpace = end
				}
				end++
				cols++
			}
			if end == len(runes) {
				lines = append(lines, string(runes[start:end]))
				break
			}
			split := end
			if lastSpace >= start {
				split = lastSpace + 1
			}
			if split == start {
				split = end
			}
			lines = append(lines, string(runes[start:split]))
			start = split
		}
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func readANSI(value string) (seq string, n int, ok bool) {
	if len(value) < 2 || value[0] != 0x1b || value[1] != '[' {
		return "", 0, false
	}
	for i := 2; i < len(value); i++ {
		if value[i] >= '@' && value[i] <= '~' {
			return value[:i+1], i + 1, true
		}
	}
	return "", 0, false
}
