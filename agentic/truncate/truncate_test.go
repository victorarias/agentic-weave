package truncate

import (
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
)

func TestHeadNoTruncation(t *testing.T) {
	input := "alpha\nbeta"
	res := Head(input, Options{MaxLines: 10, MaxBytes: 100})
	if res.Truncated {
		t.Fatalf("expected no truncation, got truncated")
	}
	if res.Content != input {
		t.Fatalf("expected content to match input")
	}
}

func TestHeadLineLimit(t *testing.T) {
	input := "one\ntwo\nthree"
	res := Head(input, Options{MaxLines: 2, MaxBytes: 100})
	if !res.Truncated || res.TruncatedBy != "lines" {
		t.Fatalf("expected truncation by lines")
	}
	if res.Content != "one\ntwo" {
		t.Fatalf("unexpected content: %q", res.Content)
	}
}

func TestHeadByteLimit(t *testing.T) {
	input := "one\ntwo"
	res := Head(input, Options{MaxLines: 10, MaxBytes: 4})
	if !res.Truncated || res.TruncatedBy != "bytes" {
		t.Fatalf("expected truncation by bytes")
	}
	if res.Content != "one" {
		t.Fatalf("unexpected content: %q", res.Content)
	}
}

func TestHeadFirstLineOverLimit(t *testing.T) {
	input := "abcdefghij\nsecond"
	res := Head(input, Options{MaxLines: 10, MaxBytes: 4})
	if !res.Truncated || res.TruncatedBy != "bytes" {
		t.Fatalf("expected truncation by bytes")
	}
	if res.Content != "abcd" {
		t.Fatalf("unexpected content: %q", res.Content)
	}
	if !res.FirstLineOverLimit {
		t.Fatalf("expected first line over limit flag")
	}
}

func TestTailLineLimit(t *testing.T) {
	input := "one\ntwo\nthree"
	res := Tail(input, Options{MaxLines: 2, MaxBytes: 100})
	if !res.Truncated || res.TruncatedBy != "lines" {
		t.Fatalf("expected truncation by lines")
	}
	if res.Content != "two\nthree" {
		t.Fatalf("unexpected content: %q", res.Content)
	}
}

func TestTailByteLimitPartial(t *testing.T) {
	input := "abcdefghij"
	res := Tail(input, Options{MaxLines: 10, MaxBytes: 4})
	if !res.Truncated || res.TruncatedBy != "bytes" {
		t.Fatalf("expected truncation by bytes")
	}
	if res.Content != "ghij" {
		t.Fatalf("unexpected content: %q", res.Content)
	}
	if !res.LastLinePartial {
		t.Fatalf("expected partial line flag")
	}
}

func TestTailByteLimitWithMaxLines(t *testing.T) {
	input := "line1\nline2\nline3"
	res := Tail(input, Options{MaxLines: 1, MaxBytes: 2})
	if !res.Truncated || res.TruncatedBy != "bytes" {
		t.Fatalf("expected truncation by bytes")
	}
	if res.Content != "e3" {
		t.Fatalf("unexpected content: %q", res.Content)
	}
	if !res.LastLinePartial {
		t.Fatalf("expected partial line flag")
	}
}

func TestTailToolResult(t *testing.T) {
	result := agentic.ToolResult{Name: "echo", Output: []byte("one\ntwo\nthree")}
	updated, meta := TailToolResult(result, Options{MaxLines: 2, MaxBytes: 100})
	if !meta.Truncated || meta.TruncatedBy != "lines" {
		t.Fatalf("expected tool output truncation")
	}
	if string(updated.Output) != "two\nthree" {
		t.Fatalf("unexpected tool output: %q", string(updated.Output))
	}
}

func TestHeadToolResultNoOutput(t *testing.T) {
	result := agentic.ToolResult{Name: "noop"}
	updated, meta := HeadToolResult(result, Options{MaxLines: 1, MaxBytes: 10})
	if meta.Truncated {
		t.Fatalf("expected no truncation metadata")
	}
	if len(updated.Output) != 0 {
		t.Fatalf("expected empty output")
	}
}
