package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/usage"
	"github.com/victorarias/agentic-weave/capabilities"
)

// Input represents a single decision request to Anthropic Claude.
// Tool calls and results should be included in History as AgentMessage entries.
type Input struct {
	SystemPrompt string
	UserMessage  string
	History      []message.AgentMessage
	Tools        []agentic.ToolDefinition
	MaxTokens    int
	Temperature  *float64
}

// Decision is the output from a single model call.
type Decision struct {
	Reply      string
	ToolCalls  []agentic.ToolCall
	StopReason string
	Usage      *usage.Usage
}

// Config controls an Anthropic client.
type Config struct {
	APIKey      string
	Model       string
	BaseURL     string
	MaxTokens   int
	Temperature *float64
	HTTPClient  *http.Client
}

// Client calls the Anthropic Messages API.
type Client struct {
	client      anthropic.Client
	model       string
	maxTokens   int
	temperature *float64
}

// New constructs an Anthropic client from config.
func New(cfg Config) (*Client, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("anthropic: api key is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, errors.New("anthropic: model is required")
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}

	client := anthropic.NewClient(opts...)

	return &Client{
		client:      client,
		model:       model,
		maxTokens:   maxTokens,
		temperature: cfg.Temperature,
	}, nil
}

// NewFromEnv builds an Anthropic client from environment variables.
func NewFromEnv() (*Client, error) {
	apiKey := envTrimmed("ANTHROPIC_API_KEY")
	model := envTrimmed("ANTHROPIC_MODEL")
	if apiKey == "" || model == "" {
		return nil, errors.New("anthropic: ANTHROPIC_API_KEY and ANTHROPIC_MODEL are required")
	}

	maxTokens := 0
	if v := envTrimmed("ANTHROPIC_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTokens = n
		}
	}

	var temperature *float64
	if v := envTrimmed("ANTHROPIC_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			temperature = &f
		}
	}

	return New(Config{
		APIKey:      apiKey,
		Model:       model,
		BaseURL:     envTrimmed("ANTHROPIC_BASE_URL"),
		MaxTokens:   maxTokens,
		Temperature: temperature,
	})
}

// Decide calls the Anthropic Messages API.
func (c *Client) Decide(ctx context.Context, input Input) (Decision, error) {
	messages := appendHistory(nil, input.History)

	userMessage := strings.TrimSpace(input.UserMessage)
	if userMessage != "" {
		messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(userMessage)))
	}

	req := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(c.maxTokens),
		Messages:  messages,
	}

	if len(input.Tools) > 0 {
		req.Tools = toolDefsToAnthropic(input.Tools)
	}

	if system := strings.TrimSpace(input.SystemPrompt); system != "" {
		req.System = []anthropic.TextBlockParam{{
			Text: system,
		}}
	}

	if input.MaxTokens > 0 {
		req.MaxTokens = int64(input.MaxTokens)
	}

	temperature := input.Temperature
	if temperature == nil {
		temperature = c.temperature
	}
	if temperature != nil {
		req.Temperature = anthropic.Float(*temperature)
	}

	msg, err := c.client.Messages.New(ctx, req)
	if err != nil {
		return Decision{}, fmt.Errorf("anthropic: %w", err)
	}

	reply, calls := parseResponse(msg)
	usageValue := capabilities.NormalizeUsage(int(msg.Usage.InputTokens), int(msg.Usage.OutputTokens), 0)

	return Decision{
		Reply:      reply,
		ToolCalls:  calls,
		StopReason: string(msg.StopReason),
		Usage:      &usageValue,
	}, nil
}

func appendHistory(messages []anthropic.MessageParam, history []message.AgentMessage) []anthropic.MessageParam {
	for _, msg := range history {
		switch msg.Role {
		case message.RoleUser:
			blocks := make([]anthropic.ContentBlockParamUnion, 0, 1)
			if strings.TrimSpace(msg.Content) != "" {
				blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewUserMessage(blocks...))
			}

		case message.RoleAssistant:
			blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(msg.ToolCalls))
			if strings.TrimSpace(msg.Content) != "" {
				blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
			}
			for _, call := range msg.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(call.ID, decodeArgs(call.Input), call.Name))
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			}

		case message.RoleTool:
			blocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.ToolResults))
			for _, result := range msg.ToolResults {
				id := strings.TrimSpace(result.ID)
				if id == "" {
					id = result.Name
				}
				content, isError := toolResultContent(result)
				blocks = append(blocks, anthropic.NewToolResultBlock(id, content, isError))
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewUserMessage(blocks...))
			}

		case message.RoleSystem:
			if strings.TrimSpace(msg.Content) != "" {
				content := "[Context Summary] " + msg.Content
				messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(content)))
			}
		}
	}
	return messages
}

func parseResponse(msg *anthropic.Message) (string, []agentic.ToolCall) {
	var reply strings.Builder
	calls := make([]agentic.ToolCall, 0)
	for _, block := range msg.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			reply.WriteString(variant.Text)
		case anthropic.ToolUseBlock:
			call := agentic.ToolCall{
				ID:    variant.ID,
				Name:  variant.Name,
				Input: variant.Input,
			}
			calls = append(calls, call)
		}
	}
	return strings.TrimSpace(reply.String()), calls
}

func toolDefsToAnthropic(tools []agentic.ToolDefinition) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		param := anthropic.ToolParam{
			Name:        tool.Name,
			InputSchema: schemaFromRaw(tool.InputSchema),
		}
		if desc := strings.TrimSpace(tool.Description); desc != "" {
			param.Description = anthropic.String(desc)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &param})
	}
	return out
}

func schemaFromRaw(raw json.RawMessage) anthropic.ToolInputSchemaParam {
	schema := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &schema)
	}

	props := schema["properties"]
	required := requiredFields(schema["required"])

	extras := map[string]any{}
	for key, value := range schema {
		switch key {
		case "properties", "required", "type":
			continue
		default:
			extras[key] = value
		}
	}

	param := anthropic.ToolInputSchemaParam{
		Properties: props,
		Required:   required,
	}
	if len(extras) > 0 {
		param.ExtraFields = extras
	}
	return param
}

func requiredFields(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func decodeArgs(input json.RawMessage) any {
	if len(input) == 0 {
		return map[string]any{}
	}
	var payload any
	if err := json.Unmarshal(input, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func toolResultContent(result agentic.ToolResult) (string, bool) {
	if result.Error != nil {
		return result.Error.Message, true
	}
	if len(result.Output) == 0 {
		return "null", false
	}
	return string(result.Output), false
}

func envTrimmed(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
