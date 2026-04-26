package openai

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strings"
)

// sseCommentStrippingTransport drops SSE "comment-only" events from
// text/event-stream responses before they reach the openai-go SDK decoder.
//
// Why: OpenRouter periodically emits keep-alive frames shaped as
//
//	: OPENROUTER PROCESSING
//	<blank line>
//
// Per the SSE spec this is a valid event with empty data, which
// EventSource clients silently discard. The openai-go decoder, however,
// hands the empty payload to json.Unmarshal and the stream blows up with
// "unexpected end of JSON input". Rather than fork the SDK or rewrite the
// streaming layer, we filter these comment-only events out at the HTTP
// boundary. The filter is a no-op for non-streaming responses and for
// streams that contain no comment lines.
type sseCommentStrippingTransport struct {
	base http.RoundTripper
}

func (t *sseCommentStrippingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "event-stream") {
		return resp, nil
	}
	resp.Body = newSSECommentFilter(resp.Body)
	return resp, nil
}

// sseCommentFilter wraps an SSE response body and strips out events whose
// every line is a comment (starts with ':'). Mixed events that contain both
// comments and data lines are passed through unchanged — the decoder ignores
// the comment lines anyway.
type sseCommentFilter struct {
	src     io.ReadCloser
	br      *bufio.Reader
	out     bytes.Buffer
	block   bytes.Buffer
	hasData bool
	eof     bool
}

func newSSECommentFilter(src io.ReadCloser) io.ReadCloser {
	return &sseCommentFilter{
		src: src,
		br:  bufio.NewReader(src),
	}
}

func (f *sseCommentFilter) Read(p []byte) (int, error) {
	for f.out.Len() == 0 && !f.eof {
		line, err := f.br.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(string(line), "\r\n")
			switch {
			case trimmed == "":
				// Event terminator. Flush only when we accumulated data.
				if f.hasData {
					f.out.Write(f.block.Bytes())
					f.out.Write(line)
				}
				f.block.Reset()
				f.hasData = false
			case strings.HasPrefix(trimmed, ":"):
				// Comment line. Keep it so a mixed event still terminates correctly,
				// but do not flip hasData.
				f.block.Write(line)
			default:
				f.block.Write(line)
				f.hasData = true
			}
		}
		switch err {
		case nil:
			continue
		case io.EOF:
			if f.block.Len() > 0 && f.hasData {
				f.out.Write(f.block.Bytes())
				f.block.Reset()
			}
			f.eof = true
		default:
			return 0, err
		}
	}
	return f.out.Read(p)
}

func (f *sseCommentFilter) Close() error {
	return f.src.Close()
}

// wrapHTTPClientWithSSEFilter returns an *http.Client whose transport drops
// SSE comment-only events from streaming responses. The original client's
// transport, timeouts, and other settings are preserved.
func wrapHTTPClientWithSSEFilter(client *http.Client) *http.Client {
	out := *client
	out.Transport = &sseCommentStrippingTransport{base: client.Transport}
	return &out
}
