// Package openai provides an OpenAI Chat Completions streaming adapter
// that implements the providers.Streamer interface.
//
// Both vanilla OpenAI and OpenAI-completions-compatible providers (OpenRouter,
// DeepSeek-direct, Fireworks, etc.) share the same wire shape, so this client
// also exposes optional extensions used by those providers:
//
//   - Reasoning + ReasoningEffort: OpenRouter-style reasoning configuration
//   - ProviderRouting: OpenRouter's "provider" routing parameter
//   - Models: OpenRouter's fallback model array
//   - Headers: caller-supplied HTTP headers (HTTP-Referer, X-Title, etc.)
//   - RequiresReasoningContentOnAssistantMessages: pad assistant history with
//     empty "reasoning_content" so DeepSeek-style multi-turn replay does not 400
//
// Reasoning ingestion accepts the streaming variants emitted by reasoning models
// behind OpenRouter ("reasoning_content", "reasoning", "reasoning_text") and
// surfaces them via providers.ReasoningDeltaEvent.
package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/providers"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

// Reasoning is the OpenRouter-shaped reasoning request parameter.
//
// Mirrors https://openrouter.ai/docs/use-cases/reasoning-tokens. Set Effort or
// MaxTokens (provider-dependent) to opt the model into reasoning. Exclude=true
// asks the provider to drop the reasoning trace from the response. Enabled is
// for providers that gate reasoning behind a boolean (DeepSeek V4 Pro is one).
type Reasoning struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Exclude   *bool  `json:"exclude,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

// ProviderRouting mirrors OpenRouter's "provider" parameter:
// https://openrouter.ai/docs/features/provider-routing.
//
// Order pins which upstream providers OpenRouter tries (and in what order).
// Setting Order disables OpenRouter's sticky routing for prompt caching, which
// is why callers who care about cache hit rate must NOT populate it. The field
// exists here because agentic-weave is a general-purpose SDK; the policy lives
// in the caller.
type ProviderRouting struct {
	Order             []string `json:"order,omitempty"`
	AllowFallbacks    *bool    `json:"allow_fallbacks,omitempty"`
	RequireParameters *bool    `json:"require_parameters,omitempty"`
	DataCollection    string   `json:"data_collection,omitempty"`
	Only              []string `json:"only,omitempty"`
	Ignore            []string `json:"ignore,omitempty"`
	Quantizations     []string `json:"quantizations,omitempty"`
	Sort              string   `json:"sort,omitempty"`
}

// Config controls an OpenAI streaming client.
type Config struct {
	APIKey          string
	Model           string
	BaseURL         string
	MaxTokens       int
	Temperature     *float64
	ReasoningEffort string // "none", "minimal", "low", "medium", "high", "xhigh"
	HTTPClient      *http.Client

	// Reasoning, when non-nil, is serialized as the top-level "reasoning" field
	// on every request. Used by OpenRouter-routed reasoning models.
	Reasoning *Reasoning

	// ProviderRouting, when non-nil, is serialized as the top-level "provider"
	// field on every request. OpenRouter only.
	ProviderRouting *ProviderRouting

	// Models is OpenRouter's fallback model array. When set, sent as the
	// top-level "models" field; the primary Model is still required as the
	// first attempt.
	Models []string

	// Headers are added to every request (e.g. HTTP-Referer, X-Title for
	// OpenRouter attribution).
	Headers http.Header

	// RequiresReasoningContentOnAssistantMessages pads each assistant history
	// message with an empty "reasoning_content" string when no
	// AgentMessage.ReasoningContent is set on it. DeepSeek V4 Pro (direct or
	// via OpenRouter) returns 400 if a multi-turn request omits
	// reasoning_content on a prior assistant turn that produced reasoning.
	// AgentMessage.ReasoningContent always wins when non-empty; the pad only
	// covers turns that didn't produce reasoning.
	RequiresReasoningContentOnAssistantMessages bool
}

// Client calls the OpenAI Chat Completions API in streaming mode
// and implements providers.Streamer.
type Client struct {
	client            oai.Client
	model             shared.ChatModel
	maxTokens         int
	temperature       *float64
	reasoningEffort   shared.ReasoningEffort
	reasoning         *Reasoning
	providerRouting   *ProviderRouting
	models            []string
	headers           http.Header
	padReasoningEmpty bool
}

type toolAccum struct {
	id       string
	name     string
	argsJSON strings.Builder
}

// New constructs an OpenAI client from config.
func New(cfg Config) (*Client, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("openai: api key is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, errors.New("openai: model is required")
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 16384
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	httpClient = wrapHTTPClientWithSSEFilter(httpClient)
	opts = append(opts, option.WithHTTPClient(httpClient))

	client := oai.NewClient(opts...)

	return &Client{
		client:            client,
		model:             shared.ChatModel(model),
		maxTokens:         maxTokens,
		temperature:       cfg.Temperature,
		reasoningEffort:   normalizeReasoningEffort(cfg.ReasoningEffort),
		reasoning:         cfg.Reasoning,
		providerRouting:   cfg.ProviderRouting,
		models:            append([]string(nil), cfg.Models...),
		headers:           cfg.Headers.Clone(),
		padReasoningEmpty: cfg.RequiresReasoningContentOnAssistantMessages,
	}, nil
}

// Stream implements providers.Streamer.
func (c *Client) Stream(ctx context.Context, input providers.Input) (<-chan providers.StreamEvent, error) {
	messages, reasonings := buildMessages(input.SystemPrompt, input.History, input.UserMessage)

	params := oai.ChatCompletionNewParams{
		Model:    c.model,
		Messages: messages,
	}

	// Set max tokens.
	maxTokens := c.maxTokens
	if input.MaxTokens > 0 {
		maxTokens = input.MaxTokens
	}
	params.MaxCompletionTokens = oai.Int(int64(maxTokens))

	// Set temperature.
	temperature := c.temperature
	if input.Temperature != nil {
		temperature = input.Temperature
	}
	if temperature != nil {
		params.Temperature = oai.Float(*temperature)
	}

	// Set reasoning effort.
	if c.reasoningEffort != "" {
		params.ReasoningEffort = c.reasoningEffort
	}

	// Add tools.
	if len(input.Tools) > 0 {
		params.Tools = toolDefsToOpenAI(input.Tools)
	}

	// Labels → OpenAI metadata.
	if len(input.Labels) > 0 {
		params.Metadata = shared.Metadata(input.Labels)
	}

	// Structured output.
	if len(input.OutputJSONSchema) > 0 {
		responseFormat, err := responseFormatFromSchema(input.OutputJSONSchema)
		if err != nil {
			return nil, err
		}
		params.ResponseFormat = responseFormat
	}

	// Request usage in stream.
	params.StreamOptions = oai.ChatCompletionStreamOptionsParam{
		IncludeUsage: oai.Bool(true),
	}

	// Per-request extensions for OpenRouter / OpenAI-completions-compatible
	// providers. Each WithJSONSet rewrites the serialized request body via sjson,
	// so they apply on top of the openai-go-rendered struct.
	reqOpts := c.requestExtensionOptions(messages, reasonings)

	stream := c.client.Chat.Completions.NewStreaming(ctx, params, reqOpts...)

	events := make(chan providers.StreamEvent, 32)
	go func() {
		defer close(events)
		c.collectStream(stream, events)
	}()

	return events, nil
}

// requestExtensionOptions builds the per-request options that inject OpenRouter-
// style extension fields (reasoning, provider routing, fallback models), custom
// headers, and per-assistant reasoning_content into the rendered request body.
//
// reasonings is a parallel slice to messages: reasonings[i] holds the
// AgentMessage.ReasoningContent that produced messages[i] (empty for non-
// assistant slots, or assistants without stored reasoning). For each assistant
// slot we inject reasoning_content when:
//   - the slot has a non-empty stored value (always — caller asked us to
//     round-trip the prior reasoning trace); or
//   - padReasoningEmpty is on (DeepSeek-style schema requirement: pad with "").
func (c *Client) requestExtensionOptions(messages []oai.ChatCompletionMessageParamUnion, reasonings []string) []option.RequestOption {
	var opts []option.RequestOption

	if c.reasoning != nil {
		opts = append(opts, option.WithJSONSet("reasoning", c.reasoning))
	}
	if c.providerRouting != nil {
		opts = append(opts, option.WithJSONSet("provider", c.providerRouting))
	}
	if len(c.models) > 0 {
		opts = append(opts, option.WithJSONSet("models", c.models))
	}
	for key, values := range c.headers {
		for _, v := range values {
			opts = append(opts, option.WithHeader(key, v))
		}
	}
	for i, msg := range messages {
		if msg.OfAssistant == nil {
			continue
		}
		var stored string
		if i < len(reasonings) {
			stored = reasonings[i]
		}
		switch {
		case stored != "":
			opts = append(opts, option.WithJSONSet(fmt.Sprintf("messages.%d.reasoning_content", i), stored))
		case c.padReasoningEmpty:
			opts = append(opts, option.WithJSONSet(fmt.Sprintf("messages.%d.reasoning_content", i), ""))
		}
	}

	return opts
}

// collectStream reads the OpenAI streaming response and emits provider events.
func (c *Client) collectStream(stream *ssestream.Stream[oai.ChatCompletionChunk], events chan<- providers.StreamEvent) {
	// Close on every exit path. The stream owns the HTTP response body; an
	// early break (e.g. mid-stream finish_reason="error") that doesn't close
	// it would leak connections back into the pool only when GC eventually
	// runs.
	defer stream.Close()

	// Tool call accumulator: OpenAI streams tool calls in deltas
	// identified by index, so we must reassemble them.
	toolAccums := map[int64]*toolAccum{}
	toolOrder := make([]int64, 0)

	var (
		finishReason string
		errMessage   string
		usageVal     *usage.Usage
	)

	for stream.Next() {
		chunk := stream.Current()

		// Usage comes on the final chunk with stream_options.include_usage.
		if chunk.Usage.TotalTokens > 0 {
			usageVal = extractUsage(chunk.Usage)
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
		}

		if choice.Delta.Content != "" {
			events <- providers.TextDeltaEvent{Delta: choice.Delta.Content}
		}

		// Reasoning delta: pulled from the delta's raw JSON because the openai-go
		// typed struct does not surface OpenRouter / DeepSeek extension fields.
		if event, ok := extractReasoningDelta(choice.Delta.RawJSON()); ok {
			events <- event
		}

		// Mid-stream error: OpenRouter signals upstream failures with
		// finish_reason: "error" plus an error blob on the chunk. Surface as
		// ErrorEvent so callers do not interpret it as a clean DoneEvent.
		if choice.FinishReason == "error" {
			errMessage = extractErrorMessage(chunk.RawJSON())
			break
		}

		for _, tc := range choice.Delta.ToolCalls {
			accum, ok := toolAccums[tc.Index]
			if !ok {
				accum = &toolAccum{}
				toolAccums[tc.Index] = accum
				toolOrder = append(toolOrder, tc.Index)
			}
			if tc.ID != "" {
				accum.id = tc.ID
			}
			if tc.Function.Name != "" {
				accum.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				accum.argsJSON.WriteString(tc.Function.Arguments)
			}
		}

		if choice.FinishReason == "tool_calls" {
			if err := emitToolUseEvents(toolAccums, toolOrder, events); err != nil {
				events <- providers.ErrorEvent{Err: err}
				return
			}
			toolAccums = map[int64]*toolAccum{}
			toolOrder = toolOrder[:0]
		}
	}

	if err := stream.Err(); err != nil {
		events <- providers.ErrorEvent{Err: fmt.Errorf("openai stream: %w", err)}
		return
	}

	if finishReason == "error" {
		if errMessage == "" {
			errMessage = "upstream error"
		}
		events <- providers.ErrorEvent{Err: fmt.Errorf("openai stream: %s", errMessage)}
		return
	}

	if finishReason == "tool_calls" {
		if err := emitToolUseEvents(toolAccums, toolOrder, events); err != nil {
			events <- providers.ErrorEvent{Err: err}
			return
		}
	}

	if finishReason == "" {
		finishReason = "stop"
	}
	events <- providers.DoneEvent{
		StopReason: normalizeStopReason(finishReason),
		Usage:      usageVal,
	}
}

// extractReasoningDelta inspects a delta's raw JSON for a reasoning field
// emitted by OpenRouter / DeepSeek / other reasoning providers. Returns the
// delta event and true when a non-empty reasoning fragment is present.
//
// The order matters: "reasoning_content" is DeepSeek's native field and the
// one we must round-trip; "reasoning" is OpenRouter's normalized field;
// "reasoning_text" is used by some less common adapters. Whichever is
// present first wins.
func extractReasoningDelta(rawDelta string) (providers.ReasoningDeltaEvent, bool) {
	if rawDelta == "" {
		return providers.ReasoningDeltaEvent{}, false
	}
	var fields struct {
		ReasoningContent json.RawMessage `json:"reasoning_content"`
		Reasoning        json.RawMessage `json:"reasoning"`
		ReasoningText    json.RawMessage `json:"reasoning_text"`
	}
	if err := json.Unmarshal([]byte(rawDelta), &fields); err != nil {
		return providers.ReasoningDeltaEvent{}, false
	}
	for _, candidate := range []struct {
		raw    json.RawMessage
		format string
	}{
		{fields.ReasoningContent, "reasoning_content"},
		{fields.Reasoning, "reasoning"},
		{fields.ReasoningText, "reasoning_text"},
	} {
		if len(candidate.raw) == 0 || string(candidate.raw) == "null" {
			continue
		}
		text := decodeReasoningPayload(candidate.raw)
		if text == "" {
			continue
		}
		return providers.ReasoningDeltaEvent{
			Delta:  text,
			Format: candidate.format,
			Raw:    append(json.RawMessage(nil), candidate.raw...),
		}, true
	}
	return providers.ReasoningDeltaEvent{}, false
}

// decodeReasoningPayload extracts the textual reasoning fragment from a
// provider-shaped blob. Most providers send a string; some wrap it in
// {"text": "..."} or an array of such objects (OpenRouter normalizes some of
// these). Falls back to an empty string when no recognizable text is found —
// the caller filters empty events.
func decodeReasoningPayload(raw json.RawMessage) string {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	var asObject struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &asObject); err == nil && asObject.Text != "" {
		return asObject.Text
	}
	var asArray []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &asArray); err == nil {
		var b strings.Builder
		for _, item := range asArray {
			b.WriteString(item.Text)
		}
		return b.String()
	}
	return ""
}

// extractUsage converts the openai-go usage struct into our provider-neutral
// shape, plus the OpenRouter cache-token reconciliation: OpenRouter reports
// `cached_tokens` as the sum of cache reads and cache writes, while we want
// only reads in CacheReadInput. When `cache_write_tokens` (or its alias
// `cache_creation_input_tokens`) is present, it is moved to CacheCreationInput
// and subtracted from CacheReadInput. On vanilla OpenAI requests these fields
// are absent and the behaviour is unchanged.
func extractUsage(u oai.CompletionUsage) *usage.Usage {
	out := usage.Usage{
		Input:  int(u.PromptTokens),
		Output: int(u.CompletionTokens),
		Total:  int(u.TotalTokens),
	}
	cached := int(u.PromptTokensDetails.CachedTokens)
	cacheWrite := extractCacheWriteTokens(u.PromptTokensDetails.RawJSON(), u.RawJSON())
	if cacheWrite > 0 {
		out.CacheCreationInput = cacheWrite
		if cached >= cacheWrite {
			cached -= cacheWrite
		}
	}
	if cached > 0 {
		out.CacheReadInput = cached
	}
	return &out
}

// extractCacheWriteTokens looks for the cache-write count in the provider's
// usage payload. OpenRouter (Anthropic-via-OpenRouter in particular) uses two
// names depending on era: `cache_write_tokens` on prompt_tokens_details, or
// `cache_creation_input_tokens` at the top level.
func extractCacheWriteTokens(promptDetailsRaw, usageRaw string) int {
	if v := readIntField(promptDetailsRaw, "cache_write_tokens"); v > 0 {
		return v
	}
	if v := readIntField(usageRaw, "cache_creation_input_tokens"); v > 0 {
		return v
	}
	return 0
}

func readIntField(rawJSON, field string) int {
	if rawJSON == "" {
		return 0
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawJSON), &fields); err != nil {
		return 0
	}
	v, ok := fields[field]
	if !ok {
		return 0
	}
	var n int
	if err := json.Unmarshal(v, &n); err != nil {
		return 0
	}
	return n
}

// extractErrorMessage pulls the upstream error message from a finish_reason=error
// chunk. OpenRouter places it under "error.message" on the choice or chunk.
func extractErrorMessage(rawChunk string) string {
	if rawChunk == "" {
		return ""
	}
	var chunk struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Choices []struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(rawChunk), &chunk); err != nil {
		return ""
	}
	if chunk.Error.Message != "" {
		return chunk.Error.Message
	}
	for _, ch := range chunk.Choices {
		if ch.Error.Message != "" {
			return ch.Error.Message
		}
	}
	return ""
}

func emitToolUseEvents(toolAccums map[int64]*toolAccum, toolOrder []int64, events chan<- providers.StreamEvent) error {
	for _, idx := range toolOrder {
		accum := toolAccums[idx]
		if accum == nil {
			continue
		}
		rawJSON := strings.TrimSpace(accum.argsJSON.String())
		if rawJSON == "" {
			rawJSON = "{}"
		}
		if !json.Valid([]byte(rawJSON)) {
			return fmt.Errorf("openai stream: invalid tool call arguments for %q", accum.name)
		}
		callID := accum.id
		if callID == "" {
			callID = fmt.Sprintf("tool_call_%d", idx)
		}
		events <- providers.ToolUseEvent{Call: agentic.ToolCall{
			ID:    callID,
			Name:  accum.name,
			Input: json.RawMessage(rawJSON),
		}}
	}
	return nil
}

func responseFormatFromSchema(schemaRaw json.RawMessage) (oai.ChatCompletionNewParamsResponseFormatUnion, error) {
	var schema map[string]any
	if err := json.Unmarshal(schemaRaw, &schema); err != nil {
		return oai.ChatCompletionNewParamsResponseFormatUnion{}, fmt.Errorf("openai: invalid output json schema: %w", err)
	}
	if len(schema) == 0 {
		return oai.ChatCompletionNewParamsResponseFormatUnion{}, errors.New("openai: output json schema is empty")
	}
	return oai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONSchema: &oai.ResponseFormatJSONSchemaParam{
			JSONSchema: oai.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:   "output",
				Schema: schema,
				Strict: oai.Bool(true),
			},
		},
	}, nil
}

// buildMessages converts the provider-agnostic input into OpenAI message params.
//
// Returns a parallel slice of per-slot reasoning content: reasonings[i] is the
// ReasoningContent of the AgentMessage that produced messages[i], or "" for
// slots without one (system prompts, user messages, tool messages, or
// assistant messages without stored reasoning). Callers use this to round-trip
// reasoning traces back to providers that require them on multi-turn replay.
func buildMessages(systemPrompt string, history []message.AgentMessage, userMessage string) ([]oai.ChatCompletionMessageParamUnion, []string) {
	messages := make([]oai.ChatCompletionMessageParamUnion, 0, len(history)+2)
	reasonings := make([]string, 0, len(history)+2)

	appendMsg := func(m oai.ChatCompletionMessageParamUnion, reasoning string) {
		messages = append(messages, m)
		reasonings = append(reasonings, reasoning)
	}

	// System prompt.
	if system := strings.TrimSpace(systemPrompt); system != "" {
		appendMsg(oai.SystemMessage(system), "")
	}

	// History.
	for i := 0; i < len(history); i++ {
		msg := history[i]
		switch msg.Role {
		case message.RoleUser:
			if strings.TrimSpace(msg.Content) != "" {
				appendMsg(oai.UserMessage(msg.Content), "")
			}

		case message.RoleAssistant:
			if len(msg.ToolCalls) == 0 {
				appendMsg(oai.AssistantMessage(msg.Content), msg.ReasoningContent)
			} else {
				// Assistant message with tool calls.
				toolCalls := make([]oai.ChatCompletionMessageToolCallUnionParam, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					toolCalls = append(toolCalls, oai.ChatCompletionMessageToolCallUnionParam{
						OfFunction: &oai.ChatCompletionMessageFunctionToolCallParam{
							ID: tc.ID,
							Function: oai.ChatCompletionMessageFunctionToolCallFunctionParam{
								Name:      tc.Name,
								Arguments: string(tc.Input),
							},
						},
					})
				}
				assistantMsg := oai.ChatCompletionAssistantMessageParam{
					ToolCalls: toolCalls,
				}
				if strings.TrimSpace(msg.Content) != "" {
					assistantMsg.Content = oai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: oai.String(msg.Content),
					}
				}
				appendMsg(oai.ChatCompletionMessageParamUnion{
					OfAssistant: &assistantMsg,
				}, msg.ReasoningContent)
			}

		case message.RoleTool:
			// Tool results: each result becomes a separate tool message.
			for _, result := range msg.ToolResults {
				content := toolResultContent(result)
				toolID := result.ID
				if toolID == "" {
					toolID = result.Name
				}
				appendMsg(oai.ToolMessage(content, toolID), "")
			}

		case message.RoleSystem:
			// Compacted history summaries should remain low-trust history, not privileged system instructions.
			if strings.TrimSpace(msg.Content) != "" {
				appendMsg(oai.UserMessage("[Context Summary] "+msg.Content), "")
			}
		}
	}

	// Current user message.
	if userMsg := strings.TrimSpace(userMessage); userMsg != "" {
		appendMsg(oai.UserMessage(userMsg), "")
	}

	return messages, reasonings
}

func toolDefsToOpenAI(tools []agentic.ToolDefinition) []oai.ChatCompletionToolUnionParam {
	out := make([]oai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		var params oai.FunctionParameters
		if len(tool.InputSchema) > 0 {
			_ = json.Unmarshal(tool.InputSchema, &params)
		}
		if params == nil {
			params = oai.FunctionParameters{"type": "object", "properties": map[string]any{}}
		}

		funcDef := oai.FunctionDefinitionParam{
			Name:       tool.Name,
			Parameters: params,
		}
		if desc := strings.TrimSpace(tool.Description); desc != "" {
			funcDef.Description = oai.String(desc)
		}

		out = append(out, oai.ChatCompletionFunctionTool(funcDef))
	}
	return out
}

func toolResultContent(result agentic.ToolResult) string {
	if result.Error != nil {
		return result.Error.Message
	}
	if len(result.Output) == 0 {
		return "null"
	}
	return string(result.Output)
}

func normalizeStopReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return reason
	}
}

func normalizeReasoningEffort(effort string) shared.ReasoningEffort {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none":
		return shared.ReasoningEffortNone
	case "minimal":
		return shared.ReasoningEffortMinimal
	case "low":
		return shared.ReasoningEffortLow
	case "medium":
		return shared.ReasoningEffortMedium
	case "high":
		return shared.ReasoningEffortHigh
	case "xhigh":
		return shared.ReasoningEffortXhigh
	default:
		return ""
	}
}
