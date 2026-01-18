package truncate

import (
	"unicode/utf8"

	"github.com/victorarias/agentic-weave/agentic"
)

const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 50 * 1024
)

// Options configures truncation thresholds.
type Options struct {
	MaxLines int
	MaxBytes int
}

// Result captures truncation output and metadata.
type Result struct {
	Content            string
	Truncated          bool
	TruncatedBy        string // "lines" | "bytes" | ""
	TotalLines         int
	TotalBytes         int
	OutputLines        int
	OutputBytes        int
	LastLinePartial    bool
	FirstLineOverLimit bool
}

// Mode controls truncation direction for tool results.
type Mode string

const (
	ModeHead Mode = "head"
	ModeTail Mode = "tail"
)

// Head truncates content from the head (keeps first N lines/bytes).
// Never returns partial lines unless the first line exceeds the byte limit.
func Head(content string, opts Options) Result {
	maxLines, maxBytes := normalize(opts)

	totalBytes := len([]byte(content))
	lines := splitLines(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return Result{
			Content:     content,
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
		}
	}

	firstLineBytes := len([]byte(lines[0]))
	if firstLineBytes > maxBytes {
		partial := truncateStringToBytes(lines[0], maxBytes)
		outputLines := 0
		if partial != "" {
			outputLines = 1
		}
		return Result{
			Content:            partial,
			Truncated:          true,
			TruncatedBy:        "bytes",
			TotalLines:         totalLines,
			TotalBytes:         totalBytes,
			OutputLines:        outputLines,
			OutputBytes:        len([]byte(partial)),
			FirstLineOverLimit: true,
		}
	}

	outLines := make([]string, 0, min(totalLines, maxLines))
	outputBytes := 0
	truncatedBy := "lines"

	for i := 0; i < totalLines && i < maxLines; i++ {
		line := lines[i]
		lineBytes := len([]byte(line))
		if i > 0 {
			lineBytes++ // newline
		}
		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}
		outLines = append(outLines, line)
		outputBytes += lineBytes
	}

	if truncatedBy != "bytes" && len(outLines) >= maxLines && outputBytes <= maxBytes {
		truncatedBy = "lines"
	}

	out := joinLines(outLines)
	return Result{
		Content:     out,
		Truncated:   true,
		TruncatedBy: truncatedBy,
		TotalLines:  totalLines,
		TotalBytes:  totalBytes,
		OutputLines: len(outLines),
		OutputBytes: len([]byte(out)),
	}
}

// Tail truncates content from the tail (keeps last N lines/bytes).
// May return a partial first line if it alone exceeds the byte limit.
func Tail(content string, opts Options) Result {
	maxLines, maxBytes := normalize(opts)

	totalBytes := len([]byte(content))
	lines := splitLines(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return Result{
			Content:     content,
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
		}
	}

	outLines := make([]string, 0, min(totalLines, maxLines))
	outputBytes := 0
	truncatedBy := "lines"
	lastLinePartial := false

	for i := totalLines - 1; i >= 0 && len(outLines) < maxLines; i-- {
		line := lines[i]
		lineBytes := len([]byte(line))
		if len(outLines) > 0 {
			lineBytes++ // newline
		}
		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(outLines) == 0 {
				partial := truncateStringToBytesFromEnd(line, maxBytes)
				outLines = append(outLines, partial)
				outputBytes += len([]byte(partial))
				lastLinePartial = true
			}
			break
		}
		outLines = append([]string{line}, outLines...)
		outputBytes += lineBytes
	}

	if truncatedBy != "bytes" && len(outLines) >= maxLines && outputBytes <= maxBytes {
		truncatedBy = "lines"
	}

	out := joinLines(outLines)
	return Result{
		Content:         out,
		Truncated:       true,
		TruncatedBy:     truncatedBy,
		TotalLines:      totalLines,
		TotalBytes:      totalBytes,
		OutputLines:     len(outLines),
		OutputBytes:     len([]byte(out)),
		LastLinePartial: lastLinePartial,
	}
}

// HeadToolResult truncates tool output from the head.
func HeadToolResult(result agentic.ToolResult, opts Options) (agentic.ToolResult, Result) {
	return truncateToolResult(result, opts, ModeHead)
}

// TailToolResult truncates tool output from the tail.
func TailToolResult(result agentic.ToolResult, opts Options) (agentic.ToolResult, Result) {
	return truncateToolResult(result, opts, ModeTail)
}

func truncateToolResult(result agentic.ToolResult, opts Options, mode Mode) (agentic.ToolResult, Result) {
	if len(result.Output) == 0 {
		return result, Result{}
	}
	content := string(result.Output)
	var res Result
	switch mode {
	case ModeHead:
		res = Head(content, opts)
	case ModeTail:
		res = Tail(content, opts)
	default:
		res = Head(content, opts)
	}
	if res.Truncated {
		result.Output = []byte(res.Content)
	}
	return result, res
}

func normalize(opts Options) (int, int) {
	maxLines := opts.MaxLines
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return maxLines, maxBytes
}

func splitLines(content string) []string {
	if content == "" {
		return []string{""}
	}
	lines := make([]string, 0, 16)
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lines = append(lines, content[start:i])
			start = i + 1
		}
	}
	lines = append(lines, content[start:])
	return lines
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	if len(lines) == 1 {
		return lines[0]
	}
	total := 0
	for _, line := range lines {
		total += len(line)
	}
	total += len(lines) - 1
	buf := make([]byte, 0, total)
	for i, line := range lines {
		if i > 0 {
			buf = append(buf, '\n')
		}
		buf = append(buf, line...)
	}
	return string(buf)
}

func truncateStringToBytesFromEnd(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	raw := []byte(value)
	if len(raw) <= maxBytes {
		return value
	}
	start := len(raw) - maxBytes
	for start < len(raw) && (raw[start]&0xC0) == 0x80 {
		start++
	}
	if start >= len(raw) {
		r, size := utf8.DecodeLastRune(raw)
		if r == utf8.RuneError && size == 0 {
			return ""
		}
		return string(raw[len(raw)-size:])
	}
	return string(raw[start:])
}

func truncateStringToBytes(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	raw := []byte(value)
	if len(raw) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.Valid(raw[:end]) {
		end--
	}
	if end <= 0 {
		return ""
	}
	return string(raw[:end])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
