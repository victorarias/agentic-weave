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
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Input represents a single decision request to Vertex Gemini.
type Input struct {
	SystemPrompt string
	UserMessage  string
	History      []HistoryTurn
	Tools        []agentic.ToolDefinition
	ToolCalls    []agentic.ToolCall
	ToolResults  []agentic.ToolResult
}

// HistoryTurn is a minimal chat history record used for request construction.
type HistoryTurn struct {
	UserMessage    string
	ToolCalls      []agentic.ToolCall
	ToolResults    []agentic.ToolResult
	AssistantReply string
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
}

// Client calls the Vertex AI Gemini REST API using ADC.
type Client struct {
	project     string
	location    string
	model       string
	baseURL     string
	temperature float64
	maxTokens   int
	client      *http.Client
	cred        oauth2.TokenSource
	pendingSig  string
}

// New constructs a Vertex Gemini client from config.
func New(cfg Config) (*Client, error) {
	project := strings.TrimSpace(cfg.Project)
	model := strings.TrimSpace(cfg.Model)
	location := strings.TrimSpace(cfg.Location)
	if location == "" {
		location = "global"
	}
	if project == "" || model == "" {
		return nil, errors.New("project and model are required")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://aiplatform.googleapis.com/v1"
	}

	ts := cfg.TokenSource
	if ts == nil {
		var err error
		ts, err = google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return nil, fmt.Errorf("vertex adc: %w", err)
		}
	}

	temp := cfg.Temperature
	if temp == 0 {
		temp = 0.2
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 65536
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
		Project:  strings.TrimSpace(os.Getenv("VERTEX_PROJECT")),
		Location: strings.TrimSpace(os.Getenv("VERTEX_LOCATION")),
		Model:    strings.TrimSpace(os.Getenv("VERTEX_MODEL")),
		BaseURL:  strings.TrimSpace(os.Getenv("VERTEX_API_BASE")),
	}
	if temp := strings.TrimSpace(os.Getenv("VERTEX_TEMPERATURE")); temp != "" {
		if v, err := strconv.ParseFloat(temp, 64); err == nil {
			cfg.Temperature = v
		}
	}
	if max := strings.TrimSpace(os.Getenv("VERTEX_MAX_TOKENS")); max != "" {
		if v, err := strconv.Atoi(max); err == nil {
			cfg.MaxTokens = v
		}
	}
	return New(cfg)
}

// Decide calls Vertex AI generateContent.
func (c *Client) Decide(ctx context.Context, input Input) (Decision, error) {
	if c.cred == nil {
		return Decision{}, errors.New("token source not configured")
	}
	reqBody, err := c.buildRequest(input)
	if err != nil {
		return Decision{}, err
	}

	endpoint := fmt.Sprintf("%s/projects/%s/locations/%s/publishers/google/models/%s:generateContent", c.baseURL, c.project, c.location, c.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	token, err := c.cred.Token()
	if err != nil {
		return Decision{}, fmt.Errorf("vertex token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

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
		return Decision{Reply: "I did not get a response. Try again."}, nil
	}

	parts := parsed.Candidates[0].Content.Parts
	toolCalls := make([]agentic.ToolCall, 0)
	var reply strings.Builder
	var reasoning strings.Builder
	pendingSig := ""
	for i, part := range parts {
		if part.FunctionCall != nil {
			args, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				args = []byte("{}")
			}
			if pendingSig == "" && part.ThoughtSignature != "" {
				pendingSig = part.ThoughtSignature
			}
			toolCalls = append(toolCalls, agentic.ToolCall{
				ID:    fmt.Sprintf("call-%d", i),
				Name:  part.FunctionCall.Name,
				Input: json.RawMessage(args),
			})
			continue
		}
		if part.Thought != "" {
			reasoning.WriteString(part.Thought)
			continue
		}
		reply.WriteString(part.Text)
	}
	if pendingSig != "" {
		c.pendingSig = pendingSig
	}

	if len(toolCalls) > 0 {
		return Decision{ToolCalls: toolCalls, Reasoning: strings.TrimSpace(reasoning.String()), FinishReason: parsed.Candidates[0].FinishReason}, nil
	}

	responseText := strings.TrimSpace(reply.String())
	if responseText == "" {
		responseText = "I am here. Tell me what you need."
	}
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
	contents := make([]vertexContent, 0, 2+len(input.ToolCalls)*2)

	contents = appendHistory(contents, input.History)

	userMessage := strings.TrimSpace(input.UserMessage)
	if userMessage == "" {
		userMessage = "Hey"
	}
	contents = append(contents, vertexContent{
		Role: "user",
		Parts: []vertexPart{{
			Text: userMessage,
		}},
	})

	for i, call := range input.ToolCalls {
		args := decodeArgs(call.Input)
		sig := ""
		if i == 0 && c.pendingSig != "" {
			sig = c.pendingSig
		}
		contents = append(contents, vertexContent{
			Role: "model",
			Parts: []vertexPart{{
				FunctionCall: &vertexFunctionCall{
					Name: call.Name,
					Args: args,
				},
				ThoughtSignature: sig,
			}},
		})

		response := toolResultPayload(input.ToolResults, i)
		contents = append(contents, vertexContent{
			Role: "user",
			Parts: []vertexPart{{
				FunctionResponse: &vertexFunctionResponse{
					Name:     call.Name,
					Response: response,
				},
			}},
		})
	}
	if c.pendingSig != "" {
		c.pendingSig = ""
	}

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
		request.Tools = []vertexTool{{FunctionDeclarations: functions}}
	}

	return json.Marshal(request)
}

func appendHistory(contents []vertexContent, history []HistoryTurn) []vertexContent {
	for _, turn := range history {
		if strings.TrimSpace(turn.UserMessage) != "" {
			contents = append(contents, vertexContent{
				Role: "user",
				Parts: []vertexPart{{
					Text: turn.UserMessage,
				}},
			})
		}
		if len(turn.ToolCalls) > 0 {
			for i, call := range turn.ToolCalls {
				contents = append(contents, vertexContent{
					Role: "model",
					Parts: []vertexPart{{
						FunctionCall: &vertexFunctionCall{
							Name: call.Name,
							Args: decodeArgs(call.Input),
						},
					}},
				})

				response := toolResultPayload(turn.ToolResults, i)
				contents = append(contents, vertexContent{
					Role: "user",
					Parts: []vertexPart{{
						FunctionResponse: &vertexFunctionResponse{
							Name:     call.Name,
							Response: response,
						},
					}},
				})
			}
		}
		if strings.TrimSpace(turn.AssistantReply) != "" {
			contents = append(contents, vertexContent{
				Role: "model",
				Parts: []vertexPart{{
					Text: turn.AssistantReply,
				}},
			})
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

func toolResultPayload(results []agentic.ToolResult, idx int) map[string]any {
	if idx >= len(results) {
		return map[string]any{"result": nil}
	}
	result := results[idx]
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
	ThoughtSignature string                  `json:"thought_signature,omitempty"`
}

// UnmarshalJSON handles both snake_case (thought_signature) and camelCase
// (thoughtSignature) variants that Vertex AI may return.
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
		if v, ok := raw["thoughtSignature"]; ok {
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

type vertexTool struct {
	FunctionDeclarations []vertexFunctionDeclaration `json:"functionDeclarations"`
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
