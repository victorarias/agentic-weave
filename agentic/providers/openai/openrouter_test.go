package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/providers"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

// captureServer starts a local HTTP server that records the inbound request
// body + headers and replies with a fixed SSE stream.
type captureServer struct {
	t          *testing.T
	server     *httptest.Server
	bodyJSON   map[string]any
	bodyRaw    []byte
	headers    http.Header
	sseChunks  []string
	sentChunks int
}

func newCaptureServer(t *testing.T, sseChunks []string) *captureServer {
	t.Helper()
	cs := &captureServer{t: t, sseChunks: sseChunks}
	cs.server = httptest.NewServer(http.HandlerFunc(cs.handle))
	t.Cleanup(cs.server.Close)
	return cs
}

func (cs *captureServer) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		cs.t.Fatalf("read request body: %v", err)
	}
	cs.bodyRaw = body
	cs.headers = r.Header.Clone()
	if err := json.Unmarshal(body, &cs.bodyJSON); err != nil {
		cs.t.Fatalf("parse request body: %v body=%s", err, string(body))
	}

	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	for _, chunk := range cs.sseChunks {
		if _, err := io.WriteString(w, chunk); err != nil {
			cs.t.Fatalf("write SSE chunk: %v", err)
		}
		if flusher != nil {
			flusher.Flush()
		}
		cs.sentChunks++
	}
}

// minimalDoneSSE returns an SSE chunk sequence that drains a stream cleanly:
// one empty delta + a usage frame + the [DONE] terminator.
func minimalDoneSSE() []string {
	return []string{
		`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}]}` + "\n\n",
		`data: {"id":"x","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}` + "\n\n",
		`data: [DONE]` + "\n\n",
	}
}

// drainStream consumes events to completion and returns them.
func drainStream(t *testing.T, c *Client, input providers.Input) []providers.StreamEvent {
	t.Helper()
	ch, err := c.Stream(context.Background(), input)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var events []providers.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

// ---------- request-body guard tests ----------

func TestStream_NoExtensionsByDefault(t *testing.T) {
	cs := newCaptureServer(t, minimalDoneSSE())
	c, err := New(Config{APIKey: "test", Model: "test", BaseURL: cs.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, c, providers.Input{UserMessage: "hi"})

	for _, key := range []string{"reasoning", "provider", "models"} {
		if _, ok := cs.bodyJSON[key]; ok {
			t.Fatalf("vanilla request must not contain %q field; body=%s", key, string(cs.bodyRaw))
		}
	}
}

func TestStream_OpenRouterReasoningField(t *testing.T) {
	cs := newCaptureServer(t, minimalDoneSSE())
	exclude := true
	c, err := New(Config{
		APIKey:    "test",
		Model:     "test",
		BaseURL:   cs.server.URL,
		Reasoning: &Reasoning{Effort: "high", Exclude: &exclude},
	})
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, c, providers.Input{UserMessage: "hi"})

	reasoning, ok := cs.bodyJSON["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("expected reasoning field as object, got %v; body=%s", cs.bodyJSON["reasoning"], string(cs.bodyRaw))
	}
	if reasoning["effort"] != "high" {
		t.Errorf("reasoning.effort = %v, want high", reasoning["effort"])
	}
	if reasoning["exclude"] != true {
		t.Errorf("reasoning.exclude = %v, want true", reasoning["exclude"])
	}
}

func TestStream_RequireParametersTrue(t *testing.T) {
	cs := newCaptureServer(t, minimalDoneSSE())
	yes := true
	c, err := New(Config{
		APIKey:          "test",
		Model:           "test",
		BaseURL:         cs.server.URL,
		ProviderRouting: &ProviderRouting{RequireParameters: &yes},
	})
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, c, providers.Input{UserMessage: "hi"})

	provider, ok := cs.bodyJSON["provider"].(map[string]any)
	if !ok {
		t.Fatalf("expected provider object; body=%s", string(cs.bodyRaw))
	}
	if provider["require_parameters"] != true {
		t.Errorf("provider.require_parameters = %v, want true", provider["require_parameters"])
	}
	if _, ok := provider["order"]; ok {
		t.Errorf("provider.order must be absent when caller did not set it; got %v", provider["order"])
	}
}

func TestStream_ProviderOrderEmittedOnlyWhenSet(t *testing.T) {
	// Sanity check: setting Order DOES surface it. Pairs with the guard above.
	cs := newCaptureServer(t, minimalDoneSSE())
	c, err := New(Config{
		APIKey:          "test",
		Model:           "test",
		BaseURL:         cs.server.URL,
		ProviderRouting: &ProviderRouting{Order: []string{"deepseek/deepseek-direct"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, c, providers.Input{UserMessage: "hi"})

	provider, _ := cs.bodyJSON["provider"].(map[string]any)
	order, ok := provider["order"].([]any)
	if !ok || len(order) != 1 || order[0] != "deepseek/deepseek-direct" {
		t.Fatalf("expected provider.order = [deepseek/deepseek-direct], got %v; body=%s", provider["order"], string(cs.bodyRaw))
	}
}

func TestStream_FallbackModelsArray(t *testing.T) {
	cs := newCaptureServer(t, minimalDoneSSE())
	c, err := New(Config{
		APIKey:  "test",
		Model:   "primary",
		BaseURL: cs.server.URL,
		Models:  []string{"primary", "fallback-a", "fallback-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, c, providers.Input{UserMessage: "hi"})

	models, ok := cs.bodyJSON["models"].([]any)
	if !ok {
		t.Fatalf("expected models array; body=%s", string(cs.bodyRaw))
	}
	if len(models) != 3 {
		t.Errorf("expected 3 models, got %d", len(models))
	}
}

func TestStream_HeadersAddedToRequest(t *testing.T) {
	cs := newCaptureServer(t, minimalDoneSSE())
	headers := http.Header{}
	headers.Set("HTTP-Referer", "https://example.com")
	headers.Set("X-Title", "agentic-weave-test")
	c, err := New(Config{
		APIKey:  "test",
		Model:   "test",
		BaseURL: cs.server.URL,
		Headers: headers,
	})
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, c, providers.Input{UserMessage: "hi"})

	if got := cs.headers.Get("HTTP-Referer"); got != "https://example.com" {
		t.Errorf("HTTP-Referer = %q, want https://example.com", got)
	}
	if got := cs.headers.Get("X-Title"); got != "agentic-weave-test" {
		t.Errorf("X-Title = %q, want agentic-weave-test", got)
	}
}

func TestStream_ReasoningContentPaddingOnAssistant(t *testing.T) {
	cs := newCaptureServer(t, minimalDoneSSE())
	c, err := New(Config{
		APIKey:  "test",
		Model:   "test",
		BaseURL: cs.server.URL,
		RequiresReasoningContentOnAssistantMessages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	history := []message.AgentMessage{
		{Role: message.RoleUser, Content: "hello"},
		{Role: message.RoleAssistant, Content: "hi"},
	}
	drainStream(t, c, providers.Input{History: history, UserMessage: "again"})

	messages, ok := cs.bodyJSON["messages"].([]any)
	if !ok {
		t.Fatalf("expected messages array; body=%s", string(cs.bodyRaw))
	}
	var foundAssistant bool
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] != "assistant" {
			continue
		}
		foundAssistant = true
		rc, present := msg["reasoning_content"]
		if !present {
			t.Errorf("assistant message missing reasoning_content; msg=%v", msg)
		}
		if rc != "" {
			t.Errorf("assistant reasoning_content = %v, want empty string", rc)
		}
	}
	if !foundAssistant {
		t.Fatal("no assistant message found in rendered request")
	}
}

func TestStream_ReasoningContentPaddingOff(t *testing.T) {
	// Default (flag false): no reasoning_content key should be injected.
	cs := newCaptureServer(t, minimalDoneSSE())
	c, err := New(Config{APIKey: "test", Model: "test", BaseURL: cs.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	history := []message.AgentMessage{
		{Role: message.RoleUser, Content: "hi"},
		{Role: message.RoleAssistant, Content: "hello"},
	}
	drainStream(t, c, providers.Input{History: history, UserMessage: "again"})

	if strings.Contains(string(cs.bodyRaw), "reasoning_content") {
		t.Errorf("rendered body must not contain reasoning_content when flag is off; body=%s", string(cs.bodyRaw))
	}
}

// ---------- reasoning ingestion tests ----------

func TestExtractReasoningDelta_AcceptsAllVariants(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantText string
		wantFmt  string
	}{
		{"reasoning_content string", `{"reasoning_content":"thinking..."}`, "thinking...", "reasoning_content"},
		{"reasoning string", `{"reasoning":"weighing options"}`, "weighing options", "reasoning"},
		{"reasoning_text string", `{"reasoning_text":"step 1"}`, "step 1", "reasoning_text"},
		{"reasoning object with text", `{"reasoning":{"text":"a thought"}}`, "a thought", "reasoning"},
		{"reasoning array of text objects", `{"reasoning":[{"text":"part one "},{"text":"part two"}]}`, "part one part two", "reasoning"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := extractReasoningDelta(tc.raw)
			if !ok {
				t.Fatalf("expected reasoning event, got none")
			}
			if ev.Delta != tc.wantText {
				t.Errorf("Delta = %q, want %q", ev.Delta, tc.wantText)
			}
			if ev.Format != tc.wantFmt {
				t.Errorf("Format = %q, want %q", ev.Format, tc.wantFmt)
			}
			if len(ev.Raw) == 0 {
				t.Errorf("Raw should be populated")
			}
		})
	}
}

func TestExtractReasoningDelta_PrefersReasoningContent(t *testing.T) {
	// reasoning_content is DeepSeek's native field and the one that must
	// round-trip on subsequent turns. When both are present, prefer it.
	raw := `{"reasoning":"normalized","reasoning_content":"native"}`
	ev, ok := extractReasoningDelta(raw)
	if !ok {
		t.Fatal("expected reasoning event")
	}
	if ev.Format != "reasoning_content" {
		t.Errorf("Format = %q, want reasoning_content", ev.Format)
	}
	if ev.Delta != "native" {
		t.Errorf("Delta = %q, want native", ev.Delta)
	}
}

func TestExtractReasoningDelta_EmptyOrMissing(t *testing.T) {
	cases := []string{
		``,
		`{}`,
		`{"reasoning":null}`,
		`{"reasoning":""}`,
		`{"content":"plain text"}`,
		`not json`,
	}
	for _, raw := range cases {
		if _, ok := extractReasoningDelta(raw); ok {
			t.Errorf("expected no event for %q", raw)
		}
	}
}

// ---------- usage / cache reconciliation tests ----------

func TestExtractCacheWriteTokens_PromptDetailsField(t *testing.T) {
	got := extractCacheWriteTokens(`{"cached_tokens":1500,"cache_write_tokens":400}`, "")
	if got != 400 {
		t.Errorf("got %d, want 400", got)
	}
}

func TestExtractCacheWriteTokens_TopLevelField(t *testing.T) {
	got := extractCacheWriteTokens("", `{"prompt_tokens":1000,"cache_creation_input_tokens":250}`)
	if got != 250 {
		t.Errorf("got %d, want 250", got)
	}
}

func TestExtractCacheWriteTokens_NotPresent(t *testing.T) {
	if got := extractCacheWriteTokens(`{"cached_tokens":100}`, `{"prompt_tokens":10}`); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

// ---------- error chunk tests ----------

func TestStream_FinishReasonErrorEmitsErrorEvent(t *testing.T) {
	chunks := []string{
		`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"error"}],"error":{"message":"upstream rate limit"}}` + "\n\n",
		`data: [DONE]` + "\n\n",
	}
	cs := newCaptureServer(t, chunks)
	c, err := New(Config{APIKey: "test", Model: "test", BaseURL: cs.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	events := drainStream(t, c, providers.Input{UserMessage: "hi"})

	var sawError bool
	for _, ev := range events {
		if e, ok := ev.(providers.ErrorEvent); ok {
			sawError = true
			if !strings.Contains(e.Err.Error(), "upstream rate limit") {
				t.Errorf("error message = %q, want it to contain upstream rate limit", e.Err.Error())
			}
		}
		if _, ok := ev.(providers.DoneEvent); ok {
			t.Errorf("must not emit DoneEvent for finish_reason=error")
		}
	}
	if !sawError {
		t.Errorf("expected ErrorEvent, got events: %#v", events)
	}
}

func TestExtractErrorMessage_TopLevel(t *testing.T) {
	got := extractErrorMessage(`{"choices":[],"error":{"message":"boom"}}`)
	if got != "boom" {
		t.Errorf("got %q, want boom", got)
	}
}

func TestExtractErrorMessage_OnChoice(t *testing.T) {
	got := extractErrorMessage(`{"choices":[{"error":{"message":"choice-level"}}]}`)
	if got != "choice-level" {
		t.Errorf("got %q, want choice-level", got)
	}
}

// ---------- reasoning streaming tests ----------

func TestStream_ReasoningDeltaEmitted(t *testing.T) {
	chunks := []string{
		`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"thinking..."}}]}` + "\n\n",
		`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}` + "\n\n",
		`data: {"id":"x","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n",
		`data: [DONE]` + "\n\n",
	}
	cs := newCaptureServer(t, chunks)
	c, err := New(Config{APIKey: "test", Model: "test", BaseURL: cs.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	events := drainStream(t, c, providers.Input{UserMessage: "hi"})

	var reasoning *providers.ReasoningDeltaEvent
	for i := range events {
		if r, ok := events[i].(providers.ReasoningDeltaEvent); ok {
			reasoning = &r
		}
	}
	if reasoning == nil {
		t.Fatalf("expected ReasoningDeltaEvent, got %#v", events)
	}
	if reasoning.Delta != "thinking..." || reasoning.Format != "reasoning_content" {
		t.Errorf("got delta=%q format=%q", reasoning.Delta, reasoning.Format)
	}
}

func TestStream_SSECommentsTolerated(t *testing.T) {
	// OpenRouter sends ": OPENROUTER PROCESSING" keep-alive comments.
	// They must not derail the stream.
	chunks := []string{
		`: OPENROUTER PROCESSING` + "\n\n",
		`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n\n",
		`data: {"id":"x","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n",
		`data: [DONE]` + "\n\n",
	}
	cs := newCaptureServer(t, chunks)
	c, err := New(Config{APIKey: "test", Model: "test", BaseURL: cs.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	events := drainStream(t, c, providers.Input{UserMessage: "hi"})

	var sawText, sawDone bool
	for _, ev := range events {
		if e, ok := ev.(providers.TextDeltaEvent); ok && e.Delta == "ok" {
			sawText = true
		}
		if _, ok := ev.(providers.DoneEvent); ok {
			sawDone = true
		}
	}
	if !sawText || !sawDone {
		t.Fatalf("expected text + done events; got %#v", events)
	}
}

func TestStream_CacheReconciliation(t *testing.T) {
	// usage reports cached_tokens that includes 250 cache writes; we want
	// CacheReadInput=750 and CacheCreationInput=250.
	chunks := []string{
		`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}]}` + "\n\n",
		`data: {"id":"x","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":10,"total_tokens":1010,"prompt_tokens_details":{"cached_tokens":1000,"cache_write_tokens":250}}}` + "\n\n",
		`data: [DONE]` + "\n\n",
	}
	cs := newCaptureServer(t, chunks)
	c, err := New(Config{APIKey: "test", Model: "test", BaseURL: cs.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	events := drainStream(t, c, providers.Input{UserMessage: "hi"})

	var u *usage.Usage
	for _, ev := range events {
		if d, ok := ev.(providers.DoneEvent); ok {
			u = d.Usage
		}
	}
	if u == nil {
		t.Fatal("missing usage on DoneEvent")
	}
	if u.CacheCreationInput != 250 {
		t.Errorf("CacheCreationInput = %d, want 250", u.CacheCreationInput)
	}
	if u.CacheReadInput != 750 {
		t.Errorf("CacheReadInput = %d, want 750", u.CacheReadInput)
	}
}

// Compile-time sanity: the new event satisfies StreamEvent.
var _ providers.StreamEvent = providers.ReasoningDeltaEvent{}
