// Package openai provides an OpenAI Chat Completions streaming adapter
// that implements the providers.Streamer interface.
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

// Config controls an OpenAI streaming client.
type Config struct {
	APIKey          string
	Model           string
	BaseURL         string
	MaxTokens       int
	Temperature     *float64
	ReasoningEffort string // "none", "minimal", "low", "medium", "high", "xhigh"
	HTTPClient      *http.Client
}

// Client calls the OpenAI Chat Completions API in streaming mode
// and implements providers.Streamer.
type Client struct {
	client          oai.Client
	model           shared.ChatModel
	maxTokens       int
	temperature     *float64
	reasoningEffort shared.ReasoningEffort
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
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}

	client := oai.NewClient(opts...)

	return &Client{
		client:          client,
		model:           shared.ChatModel(model),
		maxTokens:       maxTokens,
		temperature:     cfg.Temperature,
		reasoningEffort: normalizeReasoningEffort(cfg.ReasoningEffort),
	}, nil
}

// Stream implements providers.Streamer.
func (c *Client) Stream(ctx context.Context, input providers.Input) (<-chan providers.StreamEvent, error) {
	messages := buildMessages(input.SystemPrompt, input.History, input.UserMessage)

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

	// Structured output.
	if len(input.OutputJSONSchema) > 0 {
		var schema map[string]any
		if err := json.Unmarshal(input.OutputJSONSchema, &schema); err == nil && len(schema) > 0 {
			params.ResponseFormat = oai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONSchema: &oai.ResponseFormatJSONSchemaParam{
					JSONSchema: oai.ResponseFormatJSONSchemaJSONSchemaParam{
						Name:   "output",
						Schema: schema,
						Strict: oai.Bool(true),
					},
				},
			}
		}
	}

	// Request usage in stream.
	params.StreamOptions = oai.ChatCompletionStreamOptionsParam{
		IncludeUsage: oai.Bool(true),
	}

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)

	events := make(chan providers.StreamEvent, 32)
	go func() {
		defer close(events)
		c.collectStream(stream, events)
	}()

	return events, nil
}

// collectStream reads the OpenAI streaming response and emits provider events.
func (c *Client) collectStream(stream *ssestream.Stream[oai.ChatCompletionChunk], events chan<- providers.StreamEvent) {
	// Tool call accumulator: OpenAI streams tool calls in deltas
	// identified by index, so we must reassemble them.
	type toolAccum struct {
		id       string
		name     string
		argsJSON strings.Builder
	}
	toolAccums := map[int64]*toolAccum{}

	var (
		finishReason string
		usageVal     *usage.Usage
	)

	for stream.Next() {
		chunk := stream.Current()

		// Usage comes on the final chunk with stream_options.include_usage.
		if chunk.Usage.TotalTokens > 0 {
			u := usage.Usage{
				Input:  int(chunk.Usage.PromptTokens),
				Output: int(chunk.Usage.CompletionTokens),
				Total:  int(chunk.Usage.TotalTokens),
			}
			// OpenAI reports cached tokens in prompt_tokens_details.
			if chunk.Usage.PromptTokensDetails.CachedTokens > 0 {
				u.CacheReadInput = int(chunk.Usage.PromptTokensDetails.CachedTokens)
			}
			usageVal = &u
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
		}

		// Text delta.
		if choice.Delta.Content != "" {
			events <- providers.TextDeltaEvent{Delta: choice.Delta.Content}
		}

		// Tool call deltas.
		for _, tc := range choice.Delta.ToolCalls {
			accum, ok := toolAccums[tc.Index]
			if !ok {
				accum = &toolAccum{}
				toolAccums[tc.Index] = accum
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

		// When we get finish_reason="tool_calls", emit all accumulated tool calls.
		if choice.FinishReason == "tool_calls" {
			for _, accum := range toolAccums {
				rawJSON := strings.TrimSpace(accum.argsJSON.String())
				if rawJSON == "" {
					rawJSON = "{}"
				}
				callID := accum.id
				if callID == "" {
					callID = accum.name
				}
				events <- providers.ToolUseEvent{Call: agentic.ToolCall{
					ID:    callID,
					Name:  accum.name,
					Input: json.RawMessage(rawJSON),
				}}
			}
			toolAccums = map[int64]*toolAccum{}
		}
	}

	if err := stream.Err(); err != nil {
		events <- providers.ErrorEvent{Err: fmt.Errorf("openai stream: %w", err)}
		return
	}

	// Emit any remaining tool calls that weren't flushed by finish_reason.
	for _, accum := range toolAccums {
		rawJSON := strings.TrimSpace(accum.argsJSON.String())
		if rawJSON == "" {
			rawJSON = "{}"
		}
		events <- providers.ToolUseEvent{Call: agentic.ToolCall{
			ID:    accum.id,
			Name:  accum.name,
			Input: json.RawMessage(rawJSON),
		}}
	}

	if finishReason == "" {
		finishReason = "stop"
	}
	events <- providers.DoneEvent{
		StopReason: normalizeStopReason(finishReason),
		Usage:      usageVal,
	}
}

// buildMessages converts the provider-agnostic input into OpenAI message params.
func buildMessages(systemPrompt string, history []message.AgentMessage, userMessage string) []oai.ChatCompletionMessageParamUnion {
	messages := make([]oai.ChatCompletionMessageParamUnion, 0, len(history)+2)

	// System prompt.
	if system := strings.TrimSpace(systemPrompt); system != "" {
		messages = append(messages, oai.SystemMessage(system))
	}

	// History.
	for i := 0; i < len(history); i++ {
		msg := history[i]
		switch msg.Role {
		case message.RoleUser:
			if strings.TrimSpace(msg.Content) != "" {
				messages = append(messages, oai.UserMessage(msg.Content))
			}

		case message.RoleAssistant:
			if len(msg.ToolCalls) == 0 {
				messages = append(messages, oai.AssistantMessage(msg.Content))
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
				messages = append(messages, oai.ChatCompletionMessageParamUnion{
					OfAssistant: &assistantMsg,
				})
			}

		case message.RoleTool:
			// Tool results: each result becomes a separate tool message.
			for _, result := range msg.ToolResults {
				content := toolResultContent(result)
				toolID := result.ID
				if toolID == "" {
					toolID = result.Name
				}
				messages = append(messages, oai.ToolMessage(content, toolID))
			}

		case message.RoleSystem:
			// Compacted history summaries.
			if strings.TrimSpace(msg.Content) != "" {
				messages = append(messages, oai.SystemMessage("[Context Summary] "+msg.Content))
			}
		}
	}

	// Current user message.
	if userMsg := strings.TrimSpace(userMessage); userMsg != "" {
		messages = append(messages, oai.UserMessage(userMsg))
	}

	return messages
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
