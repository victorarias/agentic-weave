package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Input represents a single decision request to Vertex Gemini.
// Tool calls and results should be included in History as AgentMessage entries.
type Input struct {
	SystemPrompt string
	UserMessage  string
	History      []message.AgentMessage
	Tools        []agentic.ToolDefinition
	GoogleSearch bool // Enable grounding with Google Search
}

// Decision is the output from a single model call.
type Decision struct {
	Reply        string
	Reasoning    string
	ToolCalls    []agentic.ToolCall
	FinishReason string
}

// Config controls a Vertex Gemini client.
type Config struct {
	Project     string
	Location    string
	Model       string
	BaseURL     string
	Temperature float64
	MaxTokens   int
	HTTPClient  *http.Client
	TokenSource oauth2.TokenSource
	APIKey      string // Optional: use API key auth instead of OAuth2
}

// Client calls the Vertex AI Gemini REST API.
type Client struct {
	project     string
	location    string
	model       string
	baseURL     string
	temperature float64
	maxTokens   int
	client      *http.Client
	cred        oauth2.TokenSource
	apiKey      string
}

// New constructs a Vertex Gemini client from config.
func New(cfg Config) (*Client, error) {
	model := strings.TrimSpace(cfg.Model)
	apiKey := strings.TrimSpace(cfg.APIKey)

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://aiplatform.googleapis.com/v1"
	}

	temp := cfg.Temperature
	if temp == 0 {
		temp = 0.2
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 65536
	}

	// API key auth: simpler setup, no project/location needed
	if apiKey != "" {
		if model == "" {
			return nil, errors.New("model is required")
		}
		return &Client{
			model:       model,
			baseURL:     strings.TrimRight(base, "/"),
			temperature: temp,
			maxTokens:   maxTokens,
			client:      client,
			apiKey:      apiKey,
		}, nil
	}

	// OAuth2 auth: requires project and location
	project := strings.TrimSpace(cfg.Project)
	location := strings.TrimSpace(cfg.Location)
	if location == "" {
		location = "global"
	}
	if project == "" || model == "" {
		return nil, errors.New("project and model are required")
	}

	ts := cfg.TokenSource
	if ts == nil {
		var err error
		ts, err = google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return nil, fmt.Errorf("vertex adc: %w", err)
		}
	}

	return &Client{
		project:     project,
		location:    location,
		model:       model,
		baseURL:     strings.TrimRight(base, "/"),
		temperature: temp,
		maxTokens:   maxTokens,
		client:      client,
		cred:        ts,
	}, nil
}

// NewFromEnv builds a Vertex Gemini client from environment variables.
func NewFromEnv() (*Client, error) {
	cfg := Config{
		Project:  envTrimmed("VERTEX_PROJECT"),
		Location: envTrimmed("VERTEX_LOCATION"),
		Model:    envTrimmed("VERTEX_MODEL"),
		BaseURL:  envTrimmed("VERTEX_API_BASE"),
		APIKey:   envTrimmed("VERTEX_AI_API_KEY"),
	}
	if temp := envTrimmed("VERTEX_TEMPERATURE"); temp != "" {
		if v, err := strconv.ParseFloat(temp, 64); err == nil {
			cfg.Temperature = v
		}
	}
	if max := envTrimmed("VERTEX_MAX_TOKENS"); max != "" {
		if v, err := strconv.Atoi(max); err == nil {
			cfg.MaxTokens = v
		}
	}
	return New(cfg)
}

func envTrimmed(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// Decide calls Vertex AI generateContent.
func (c *Client) Decide(ctx context.Context, input Input) (Decision, error) {
	if c.cred == nil && c.apiKey == "" {
		return Decision{}, errors.New("no auth configured (need token source or API key)")
	}
	reqBody, err := c.buildRequest(input)
	if err != nil {
		return Decision{}, err
	}

	var endpoint string
	if c.apiKey != "" {
		// API key auth uses the publishers endpoint
		endpoint = fmt.Sprintf("%s/publishers/google/models/%s:generateContent?key=%s", c.baseURL, c.model, c.apiKey)
	} else {
		// OAuth2 auth uses the project/location endpoint
		endpoint = fmt.Sprintf("%s/projects/%s/locations/%s/publishers/google/models/%s:generateContent", c.baseURL, c.project, c.location, c.model)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Set auth header for OAuth2 (not needed for API key - it's in the URL)
	if c.cred != nil {
		token, err := c.cred.Token()
		if err != nil {
			return Decision{}, fmt.Errorf("vertex token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return Decision{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := readResponseBody(resp)
		return Decision{}, fmt.Errorf("vertex gemini error: status %d: %s", resp.StatusCode, body)
	}

	var parsed vertexResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return Decision{}, err
	}
	if len(parsed.Candidates) == 0 {
		return Decision{}, errors.New("vertex gemini: no candidates in response")
	}

	parts := parsed.Candidates[0].Content.Parts
	toolCalls := make([]agentic.ToolCall, 0)
	var reply strings.Builder
	var reasoning strings.Builder
	for i, part := range parts {
		if part.FunctionCall != nil {
			args, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				args = []byte("{}")
			}
			// Capture signature directly from each part. Per Vertex AI docs:
			// - Parallel calls: only first functionCall part has signature
			// - Sequential calls: each step has its own signature
			toolCalls = append(toolCalls, agentic.ToolCall{
				ID:               fmt.Sprintf("call-%d", i),
				Name:             part.FunctionCall.Name,
				Input:            json.RawMessage(args),
				ThoughtSignature: part.ThoughtSignature,
			})
			continue
		}
		if part.Thought != "" {
			reasoning.WriteString(part.Thought)
			continue
		}
		reply.WriteString(part.Text)
	}

	if len(toolCalls) > 0 {
		return Decision{ToolCalls: toolCalls, Reasoning: strings.TrimSpace(reasoning.String()), FinishReason: parsed.Candidates[0].FinishReason}, nil
	}

	responseText := strings.TrimSpace(reply.String())
	if parsed.Candidates[0].FinishReason == "MAX_TOKENS" {
		responseText += "\n\n(Reply may be truncated. Consider increasing VERTEX_MAX_TOKENS.)"
	}
	candidateThoughts := strings.TrimSpace(parsed.Candidates[0].Thoughts)
	reasoningText := strings.TrimSpace(reasoning.String())
	if reasoningText == "" {
		reasoningText = candidateThoughts
	}
	return Decision{
		Reply:        responseText,
		Reasoning:    reasoningText,
		FinishReason: parsed.Candidates[0].FinishReason,
	}, nil
}

func readResponseBody(resp *http.Response) (string, error) {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	body := strings.TrimSpace(string(data))
	if body == "" {
		return "<empty body>", nil
	}
	if len(body) > 1200 {
		return body[:1200] + "... (truncated)", nil
	}
	return body, nil
}

func (c *Client) buildRequest(input Input) ([]byte, error) {
	contents := make([]vertexContent, 0, 2+len(input.History))

	contents = appendHistory(contents, input.History)

	// Add user message if provided.
	// When history ends with a tool result and no new user message is provided,
	// the model resumes thinking directly after the function response.
	userMessage := strings.TrimSpace(input.UserMessage)
	if userMessage != "" {
		contents = append(contents, vertexContent{
			Role: "user",
			Parts: []vertexPart{{
				Text: userMessage,
			}},
		})
	}

	// Tool calls and results are serialized from input.History.
	// Include all tool data in History as AgentMessage entries.

	functions := make([]vertexFunctionDeclaration, 0, len(input.Tools))
	for _, tool := range input.Tools {
		functions = append(functions, vertexFunctionDeclaration{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  ensureSchema(tool.InputSchema),
		})
	}

	request := vertexRequest{
		Contents: contents,
		GenerationConfig: vertexGenerationConfig{
			Temperature:     c.temperature,
			MaxOutputTokens: c.maxTokens,
		},
	}
	if strings.TrimSpace(input.SystemPrompt) != "" {
		request.SystemInstruction = vertexSystemInstruction{
			Parts: []vertexPart{{Text: input.SystemPrompt}},
		}
	}

	if len(functions) > 0 {
		request.Tools = append(request.Tools, vertexTool{FunctionDeclarations: functions})
	}
	if input.GoogleSearch {
		request.Tools = append(request.Tools, vertexTool{GoogleSearch: &vertexGoogleSearch{}})
	}

	return json.Marshal(request)
}

// appendHistory converts AgentMessage history to Vertex AI content format.
func appendHistory(contents []vertexContent, history []message.AgentMessage) []vertexContent {
	for _, msg := range history {
		switch msg.Role {
		case message.RoleUser:
			if strings.TrimSpace(msg.Content) != "" {
				contents = append(contents, vertexContent{
					Role: "user",
					Parts: []vertexPart{{
						Text: msg.Content,
					}},
				})
			}

		case message.RoleAssistant:
			// Handle assistant text replies
			if strings.TrimSpace(msg.Content) != "" {
				contents = append(contents, vertexContent{
					Role: "model",
					Parts: []vertexPart{{
						Text: msg.Content,
					}},
				})
			}
			// Handle assistant messages with tool calls
			if len(msg.ToolCalls) > 0 {
				for _, call := range msg.ToolCalls {
					contents = append(contents, vertexContent{
						Role: "model",
						Parts: []vertexPart{{
							FunctionCall: &vertexFunctionCall{
								Name: call.Name,
								Args: decodeArgs(call.Input),
							},
							ThoughtSignature: call.ThoughtSignature,
						}},
					})
				}
			}

		case message.RoleTool:
			// Tool results become function responses
			for _, result := range msg.ToolResults {
				response := singleToolResultPayload(result)
				contents = append(contents, vertexContent{
					Role: "user",
					Parts: []vertexPart{{
						FunctionResponse: &vertexFunctionResponse{
							Name:     result.Name,
							Response: response,
						},
					}},
				})
			}

		case message.RoleSystem:
			// System messages in history are typically summaries from compaction
			if strings.TrimSpace(msg.Content) != "" {
				contents = append(contents, vertexContent{
					Role: "user",
					Parts: []vertexPart{{
						Text: "[Context Summary] " + msg.Content,
					}},
				})
			}
		}
	}
	return contents
}

func decodeArgs(input json.RawMessage) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return map[string]any{}
	}
	return args
}

func singleToolResultPayload(result agentic.ToolResult) map[string]any {
	if result.Error != nil {
		return map[string]any{"error": result.Error.Message}
	}
	if len(result.Output) == 0 {
		return map[string]any{"result": nil}
	}
	var payload any
	if err := json.Unmarshal(result.Output, &payload); err != nil {
		return map[string]any{"result": string(result.Output)}
	}
	return map[string]any{"result": payload}
}

func ensureSchema(schema json.RawMessage) map[string]any {
	if len(schema) == 0 {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	return parsed
}

type vertexRequest struct {
	SystemInstruction vertexSystemInstruction `json:"system_instruction,omitempty"`
	Contents          []vertexContent         `json:"contents"`
	Tools             []vertexTool            `json:"tools,omitempty"`
	GenerationConfig  vertexGenerationConfig  `json:"generationConfig,omitempty"`
}

type vertexSystemInstruction struct {
	Parts []vertexPart `json:"parts,omitempty"`
}

type vertexContent struct {
	Role  string       `json:"role"`
	Parts []vertexPart `json:"parts"`
}

type vertexPart struct {
	Text             string                  `json:"text,omitempty"`
	Thought          string                  `json:"thought,omitempty"`
	FunctionCall     *vertexFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *vertexFunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
}

// UnmarshalJSON handles both camelCase (thoughtSignature) and snake_case
// (thought_signature) variants that Vertex AI may return in responses.
func (p *vertexPart) UnmarshalJSON(data []byte) error {
	type alias vertexPart
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var base alias
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	*p = vertexPart(base)
	if p.ThoughtSignature == "" {
		if v, ok := raw["thought_signature"]; ok {
			_ = json.Unmarshal(v, &p.ThoughtSignature)
		}
	}
	return nil
}

type vertexFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type vertexFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type vertexGoogleSearch struct{}

type vertexTool struct {
	FunctionDeclarations []vertexFunctionDeclaration `json:"functionDeclarations,omitempty"`
	GoogleSearch         *vertexGoogleSearch         `json:"googleSearch,omitempty"`
}

type vertexFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type vertexGenerationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type vertexResponse struct {
	Candidates []vertexCandidate `json:"candidates"`
}

type vertexCandidate struct {
	Content      vertexContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
	Thoughts     string        `json:"thoughts,omitempty"`
}
