package openai

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func filterSSE(t *testing.T, in string) string {
	t.Helper()
	r := newSSECommentFilter(io.NopCloser(strings.NewReader(in)))
	t.Cleanup(func() { _ = r.Close() })
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(out)
}

func TestSSECommentFilter_DropsCommentOnlyEvent(t *testing.T) {
	in := ": OPENROUTER PROCESSING\n\n" +
		"data: {\"a\":1}\n\n"
	want := "data: {\"a\":1}\n\n"
	if got := filterSSE(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSSECommentFilter_KeepsCommentInsideMixedEvent(t *testing.T) {
	// Comment + data in the same event block: pass through unchanged so the
	// SSE decoder reaches the data line.
	in := ": pre-data\ndata: {\"a\":1}\n\n"
	if got := filterSSE(t, in); got != in {
		t.Errorf("mixed event must be passed through unchanged; got %q", got)
	}
}

func TestSSECommentFilter_LeavesNonStreamUnchanged(t *testing.T) {
	in := "data: {\"a\":1}\n\ndata: {\"b\":2}\n\n"
	if got := filterSSE(t, in); got != in {
		t.Errorf("got %q, want unchanged input", got)
	}
}

func TestSSECommentFilter_HandlesMultipleConsecutiveCommentEvents(t *testing.T) {
	in := ": one\n\n" + ": two\n\n" + "data: {\"x\":1}\n\n"
	want := "data: {\"x\":1}\n\n"
	if got := filterSSE(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSSECommentFilter_HandlesLineLongerThanBufioBuffer(t *testing.T) {
	// A single SSE data line that exceeds the default bufio buffer (4 KiB)
	// would cause bufio.Reader.ReadBytes to internally see ErrBufferFull
	// across multiple ReadSlice calls. Confirm the filter coalesces those
	// reads instead of aborting the stream when one line is huge — e.g. a
	// tool-call JSON arguments blob.
	const bufSize = 16
	huge := strings.Repeat("x", bufSize*8) // far larger than the small bufio buffer
	in := "data: " + huge + "\n\n"

	r := &sseCommentFilter{
		src: io.NopCloser(strings.NewReader(in)),
		br:  bufio.NewReaderSize(strings.NewReader(in), bufSize),
	}
	t.Cleanup(func() { _ = r.Close() })
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(out) != in {
		t.Errorf("got %d bytes, want %d (long line should pass through unchanged)", len(out), len(in))
	}
}

func TestSSECommentFilter_FlushesTrailingDataWithoutTerminator(t *testing.T) {
	// Some servers omit the final blank line. Our filter should still emit
	// any pending data event (the SDK decoder will surface stream.Err()
	// for malformed terminators on its own).
	in := ": k\n\ndata: {\"x\":1}"
	want := "data: {\"x\":1}"
	if got := filterSSE(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
