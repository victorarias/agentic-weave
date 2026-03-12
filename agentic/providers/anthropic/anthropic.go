package anthropic

import (
	"context"
	"encoding/base64"
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
	SystemPrompt     string
	UserMessage      string
	History          []message.AgentMessage
	Tools            []agentic.ToolDefinition
	ToolChoice       *ToolChoice
	OutputJSONSchema json.RawMessage
	MaxTokens        int
	Temperature      *float64
	ThinkingMode     string
	ThinkingEffort   string
	ThinkingBgt      int
}

// ToolChoice controls Anthropic tool selection behavior.
//
// Mode values:
//   - "auto" (default): model chooses whether to call a tool.
//   - "any": model must call at least one tool.
//   - "none": model must not call tools.
//   - "tool": model must call the specific tool in Name.
//
// Name is required when Mode is "tool".
type ToolChoice struct {
	Mode string
	Name string
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
	// ThinkingMode controls extended thinking mode: adaptive, fixed, or off.
	ThinkingMode string
	// ThinkingEffort only applies when ThinkingMode is adaptive.
	ThinkingEffort string
	// ThinkingBgt only applies when ThinkingMode is fixed/enabled.
	ThinkingBgt int
	HTTPClient  *http.Client

	// CacheTTL sets the time-to-live for prompt cache breakpoints.
	// Valid values: "" (default 5m), "5m", "1h".
	// The 1h TTL costs more per cache write but keeps entries cached
	// longer, which reduces misses for conversations with longer
	// inter-turn gaps.
	CacheTTL string
}

// CacheMode controls how prompt caching breakpoints are applied.
type CacheMode int

const (
	// CacheModeAutomatic uses the top-level cache_control field introduced in
	// SDK v1.26. The API places a single breakpoint on the last cacheable block
	// and advances it automatically as conversations grow.
	//
	// Supported on the Claude API and Azure AI Foundry.
	// NOT yet supported on Amazon Bedrock or Google Vertex AI.
	CacheModeAutomatic CacheMode = iota

	// CacheModeExplicit sets cache_control on individual content blocks:
	//   1. Last system prompt block
	//   2. Last tool definition
	//   3. Last content block in the final history message
	//
	// Supported on all platforms including Vertex AI and Bedrock.
	CacheModeExplicit
)

// Client calls the Anthropic Messages API.
type Client struct {
	client         anthropic.Client
	model          string
	maxTokens      int
	temperature    *float64
	thinkingMode   string
	thinkingEffort string
	thinkingBgt    int64
	cacheMode      CacheMode
	cacheTTL       anthropic.CacheControlEphemeralTTL
}

const (
	thinkingModeAdaptive = "adaptive"
	thinkingModeFixed    = "fixed"
	thinkingModeOff      = "off"

	thinkingEffortLow    = "low"
	thinkingEffortMedium = "medium"
	thinkingEffortHigh   = "high"
	thinkingEffortMax    = "max"
)

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
	thinkingMode := normalizeThinkingMode(cfg.ThinkingMode)
	thinkingEffort := normalizeThinkingEffort(cfg.ThinkingEffort)
	thinkingBgt := int64(cfg.ThinkingBgt)
	if thinkingMode == thinkingModeFixed && thinkingBgt < 1024 {
		thinkingBgt = 1024
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
		client:         client,
		model:          model,
		maxTokens:      maxTokens,
		temperature:    cfg.Temperature,
		thinkingMode:   thinkingMode,
		thinkingEffort: thinkingEffort,
		thinkingBgt:    thinkingBgt,
		cacheMode:      CacheModeAutomatic,
		cacheTTL:       parseCacheTTL(cfg.CacheTTL),
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
	thinkingMode := envTrimmed("ANTHROPIC_THINKING_MODE")
	thinkingEffort := envTrimmed("ANTHROPIC_THINKING_EFFORT")
	thinkingBgt := 0
	if v := envTrimmed("ANTHROPIC_THINKING_BUDGET_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			thinkingBgt = n
		}
	}

	return New(Config{
		APIKey:         apiKey,
		Model:          model,
		BaseURL:        envTrimmed("ANTHROPIC_BASE_URL"),
		MaxTokens:      maxTokens,
		Temperature:    temperature,
		ThinkingMode:   thinkingMode,
		ThinkingEffort: thinkingEffort,
		ThinkingBgt:    thinkingBgt,
		CacheTTL:       envTrimmed("ANTHROPIC_CACHE_TTL"),
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
	if err := applyToolChoice(&req, input.ToolChoice); err != nil {
		return Decision{}, err
	}
	if err := applyOutputJSONSchema(&req, input.OutputJSONSchema); err != nil {
		return Decision{}, err
	}

	if system := strings.TrimSpace(input.SystemPrompt); system != "" {
		req.System = []anthropic.TextBlockParam{{
			Text: system,
		}}
	}

	applyPromptCaching(&req, c.cacheMode, c.cacheTTL)

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
	mode, effort, budget := c.resolveThinking(input.ThinkingMode, input.ThinkingEffort, input.ThinkingBgt)
	applyThinkingConfig(&req, mode, effort, budget)

	msg, err := c.client.Messages.New(ctx, req)
	if err != nil {
		return Decision{}, fmt.Errorf("anthropic: %w", err)
	}

	reply, calls := parseResponse(msg)
	usageValue := capabilities.NormalizeUsageWithCache(
		int(msg.Usage.InputTokens),
		int(msg.Usage.OutputTokens),
		0,
		int(msg.Usage.CacheReadInputTokens),
		int(msg.Usage.CacheCreationInputTokens),
	)

	return Decision{
		Reply:      reply,
		ToolCalls:  calls,
		StopReason: string(msg.StopReason),
		Usage:      &usageValue,
	}, nil
}

func appendHistory(messages []anthropic.MessageParam, history []message.AgentMessage) []anthropic.MessageParam {
	for i := 0; i < len(history); i++ {
		msg := history[i]
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
			// Anthropic requires that all tool_result blocks for a given tool_use message
			// appear in the *immediately next* message. Our loop records tool results as
			// separate role=tool history entries (often one per tool call), so we coalesce
			// consecutive tool messages into a single user message containing multiple
			// tool_result blocks.
			blocks := make([]anthropic.ContentBlockParamUnion, 0)
			for ; i < len(history); i++ {
				if history[i].Role != message.RoleTool {
					break
				}
				for _, result := range history[i].ToolResults {
					id := strings.TrimSpace(result.ID)
					if id == "" {
						id = result.Name
					}
					blocks = append(blocks, toolResultToBlock(result, id))
				}
			}
			i-- // compensate for the outer loop increment
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

func toolResultToBlock(result agentic.ToolResult, id string) anthropic.ContentBlockParamUnion {
	content, isError := toolResultContent(result)
	if len(result.InlineData) == 0 {
		return anthropic.NewToolResultBlock(id, content, isError)
	}

	parts := make([]anthropic.ToolResultBlockParamContentUnion, 0, 1+len(result.InlineData))
	parts = append(parts, anthropic.ToolResultBlockParamContentUnion{
		OfText: &anthropic.TextBlockParam{Text: content},
	})
	for _, data := range result.InlineData {
		parts = append(parts, anthropic.ToolResultBlockParamContentUnion{
			OfImage: &anthropic.ImageBlockParam{
				Source: anthropic.ImageBlockParamSourceUnion{
					OfBase64: &anthropic.Base64ImageSourceParam{
						Data:      base64.StdEncoding.EncodeToString(data.Data),
						MediaType: anthropic.Base64ImageSourceMediaType(data.MIMEType),
					},
				},
			},
		})
	}
	return anthropic.ContentBlockParamUnion{
		OfToolResult: &anthropic.ToolResultBlockParam{
			ToolUseID: id,
			Content:   parts,
			IsError:   anthropic.Bool(isError),
		},
	}
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

func applyToolChoice(req *anthropic.MessageNewParams, choice *ToolChoice) error {
	if req == nil || choice == nil {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(choice.Mode))
	switch mode {
	case "", "auto":
		req.ToolChoice = anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{Type: "auto"}}
	case "any":
		req.ToolChoice = anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{Type: "any"}}
	case "none":
		none := anthropic.NewToolChoiceNoneParam()
		req.ToolChoice = anthropic.ToolChoiceUnionParam{OfNone: &none}
	case "tool":
		name := strings.TrimSpace(choice.Name)
		if name == "" {
			return errors.New("anthropic: tool choice mode 'tool' requires a tool name")
		}
		req.ToolChoice = anthropic.ToolChoiceParamOfTool(name)
	default:
		return fmt.Errorf("anthropic: unsupported tool choice mode %q", choice.Mode)
	}
	return nil
}

func applyOutputJSONSchema(req *anthropic.MessageNewParams, schemaRaw json.RawMessage) error {
	if req == nil || len(schemaRaw) == 0 {
		return nil
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaRaw, &schema); err != nil {
		return fmt.Errorf("anthropic: invalid output json schema: %w", err)
	}
	if len(schema) == 0 {
		return errors.New("anthropic: output json schema is empty")
	}

	req.OutputConfig = anthropic.OutputConfigParam{
		Format: anthropic.JSONOutputFormatParam{Schema: schema},
	}
	return nil
}

func envTrimmed(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// parseCacheTTL normalises a TTL string into an SDK constant.
// An empty string means "use API default" (5 minutes).
func parseCacheTTL(raw string) anthropic.CacheControlEphemeralTTL {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1h":
		return anthropic.CacheControlEphemeralTTLTTL1h
	case "5m":
		return anthropic.CacheControlEphemeralTTLTTL5m
	default:
		// Empty → omitzero → API default (5m).
		return ""
	}
}

func normalizeThinkingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "off", "disabled", "none":
		return thinkingModeOff
	case "adaptive":
		return thinkingModeAdaptive
	case "fixed", "enabled":
		return thinkingModeFixed
	default:
		return thinkingModeOff
	}
}

func normalizeThinkingEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "":
		return ""
	case thinkingEffortLow, thinkingEffortMedium, thinkingEffortHigh, thinkingEffortMax:
		return strings.ToLower(strings.TrimSpace(effort))
	default:
		return ""
	}
}

func (c *Client) resolveThinking(modeOverride string, effortOverride string, budgetOverride int) (string, string, int64) {
	mode := c.thinkingMode
	if strings.TrimSpace(modeOverride) != "" {
		mode = normalizeThinkingMode(modeOverride)
	}
	effort := c.thinkingEffort
	if strings.TrimSpace(effortOverride) != "" {
		effort = normalizeThinkingEffort(effortOverride)
	}
	budget := c.thinkingBgt
	if budgetOverride > 0 {
		budget = int64(budgetOverride)
	}
	if mode == thinkingModeFixed && budget < 1024 {
		budget = 1024
	}
	if mode != thinkingModeAdaptive {
		effort = ""
	}
	return mode, effort, budget
}

func applyThinkingConfig(req *anthropic.MessageNewParams, mode string, effort string, budget int64) {
	if req == nil {
		return
	}
	switch mode {
	case thinkingModeOff:
		disabled := anthropic.NewThinkingConfigDisabledParam()
		req.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: &disabled}
	case thinkingModeFixed:
		fixedBudget := budget
		if fixedBudget < 1024 {
			fixedBudget = 1024
		}
		req.Thinking = anthropic.ThinkingConfigParamOfEnabled(fixedBudget)
	default:
		adaptive := anthropic.NewThinkingConfigAdaptiveParam()
		if normalizedEffort := normalizeThinkingEffort(effort); normalizedEffort != "" {
			adaptive.SetExtraFields(map[string]any{"effort": normalizedEffort})
		}
		req.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}
	}
}
