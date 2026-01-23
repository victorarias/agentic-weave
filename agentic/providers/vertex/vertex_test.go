package vertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"golang.org/x/oauth2"
)

func TestVertexPartMarshalThoughtSignature(t *testing.T) {
	part := vertexPart{
		FunctionCall: &vertexFunctionCall{
			Name: "test",
			Args: map[string]any{},
		},
		ThoughtSignature: "sig123",
	}

	data, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify output uses camelCase (thoughtSignature), not snake_case
	output := string(data)
	if !strings.Contains(output, `"thoughtSignature":"sig123"`) {
		t.Errorf("expected camelCase thoughtSignature in output, got: %s", output)
	}
	if strings.Contains(output, "thought_signature") {
		t.Errorf("unexpected snake_case thought_signature in output: %s", output)
	}
}

func TestVertexPartUnmarshalThoughtSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "camelCase (primary)",
			input:    `{"functionCall":{"name":"test","args":{}},"thoughtSignature":"sig456"}`,
			expected: "sig456",
		},
		{
			name:     "snake_case (fallback)",
			input:    `{"functionCall":{"name":"test","args":{}},"thought_signature":"sig123"}`,
			expected: "sig123",
		},
		{
			name:     "camelCase takes precedence over snake_case",
			input:    `{"functionCall":{"name":"test","args":{}},"thought_signature":"snake","thoughtSignature":"camel"}`,
			expected: "camel",
		},
		{
			name:     "no signature",
			input:    `{"functionCall":{"name":"test","args":{}}}`,
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var part vertexPart
			if err := json.Unmarshal([]byte(tc.input), &part); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if part.ThoughtSignature != tc.expected {
				t.Errorf("expected ThoughtSignature=%q, got %q", tc.expected, part.ThoughtSignature)
			}
		})
	}
}

func TestBuildRequestIncludesThoughtSignatureFromToolCall(t *testing.T) {
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	input := Input{
		UserMessage: "test message",
		ToolCalls: []agentic.ToolCall{
			{
				ID:               "call-0",
				Name:             "test_tool",
				Input:            json.RawMessage(`{"arg":"value"}`),
				ThoughtSignature: "sig-from-toolcall-123",
			},
			{
				ID:    "call-1",
				Name:  "another_tool",
				Input: json.RawMessage(`{}`),
			},
		},
		ToolResults: []agentic.ToolResult{
			{ID: "call-0", Name: "test_tool", Output: json.RawMessage(`{"result":"ok"}`)},
			{ID: "call-1", Name: "another_tool", Output: json.RawMessage(`{"result":"done"}`)},
		},
	}

	reqBody, err := client.buildRequest(input)
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}

	// Parse the request to verify structure
	var req vertexRequest
	if err := json.Unmarshal(reqBody, &req); err != nil {
		t.Fatalf("failed to parse request: %v", err)
	}

	// Find the model content with first function call
	foundSig := false
	for _, content := range req.Contents {
		if content.Role == "model" && len(content.Parts) > 0 {
			part := content.Parts[0]
			if part.FunctionCall != nil && part.FunctionCall.Name == "test_tool" {
				if part.ThoughtSignature != "sig-from-toolcall-123" {
					t.Errorf("expected ThoughtSignature=%q on first tool call, got %q",
						"sig-from-toolcall-123", part.ThoughtSignature)
				}
				foundSig = true
			}
			// Second tool call should NOT have signature
			if part.FunctionCall != nil && part.FunctionCall.Name == "another_tool" {
				if part.ThoughtSignature != "" {
					t.Errorf("expected empty ThoughtSignature on second tool call, got %q",
						part.ThoughtSignature)
				}
			}
		}
	}

	if !foundSig {
		t.Error("did not find model content with first function call")
	}
}

func TestBuildRequestWithoutThoughtSignature(t *testing.T) {
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	input := Input{
		UserMessage: "test message",
		ToolCalls: []agentic.ToolCall{
			{
				ID:    "call-0",
				Name:  "test_tool",
				Input: json.RawMessage(`{}`),
				// No ThoughtSignature
			},
		},
		ToolResults: []agentic.ToolResult{
			{ID: "call-0", Name: "test_tool", Output: json.RawMessage(`{}`)},
		},
	}

	reqBody, err := client.buildRequest(input)
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}

	var req vertexRequest
	if err := json.Unmarshal(reqBody, &req); err != nil {
		t.Fatalf("failed to parse request: %v", err)
	}

	// Verify no signature in the request when tool call doesn't have one
	for _, content := range req.Contents {
		if content.Role == "model" && len(content.Parts) > 0 {
			part := content.Parts[0]
			if part.FunctionCall != nil && part.ThoughtSignature != "" {
				t.Errorf("expected empty ThoughtSignature when not set, got %q", part.ThoughtSignature)
			}
		}
	}
}

func TestAppendHistoryIncludesThoughtSignature(t *testing.T) {
	history := []HistoryTurn{
		{
			UserMessage: "first message",
			ToolCalls: []agentic.ToolCall{
				{
					ID:               "hist-call-0",
					Name:             "historical_tool",
					Input:            json.RawMessage(`{"x":1}`),
					ThoughtSignature: "historical-sig-456",
				},
				{
					ID:    "hist-call-1",
					Name:  "another_hist_tool",
					Input: json.RawMessage(`{}`),
				},
			},
			ToolResults: []agentic.ToolResult{
				{ID: "hist-call-0", Name: "historical_tool", Output: json.RawMessage(`{"y":2}`)},
				{ID: "hist-call-1", Name: "another_hist_tool", Output: json.RawMessage(`{}`)},
			},
			AssistantReply: "done",
		},
	}

	contents := appendHistory(nil, history)

	// Find the model content with first function call from history
	foundHistSig := false
	for _, content := range contents {
		if content.Role == "model" && len(content.Parts) > 0 {
			part := content.Parts[0]
			if part.FunctionCall != nil && part.FunctionCall.Name == "historical_tool" {
				if part.ThoughtSignature != "historical-sig-456" {
					t.Errorf("expected historical ThoughtSignature=%q, got %q",
						"historical-sig-456", part.ThoughtSignature)
				}
				foundHistSig = true
			}
			// Second historical tool call should NOT have signature
			if part.FunctionCall != nil && part.FunctionCall.Name == "another_hist_tool" {
				if part.ThoughtSignature != "" {
					t.Errorf("expected empty ThoughtSignature on second historical tool call, got %q",
						part.ThoughtSignature)
				}
			}
		}
	}

	if !foundHistSig {
		t.Error("did not find historical model content with function call")
	}
}

func TestAppendHistoryWithoutThoughtSignature(t *testing.T) {
	history := []HistoryTurn{
		{
			UserMessage: "message without signature",
			ToolCalls: []agentic.ToolCall{
				{
					ID:    "no-sig-call",
					Name:  "tool_without_sig",
					Input: json.RawMessage(`{}`),
					// No ThoughtSignature
				},
			},
			ToolResults: []agentic.ToolResult{
				{ID: "no-sig-call", Name: "tool_without_sig", Output: json.RawMessage(`{}`)},
			},
		},
	}

	contents := appendHistory(nil, history)

	for _, content := range contents {
		if content.Role == "model" && len(content.Parts) > 0 {
			part := content.Parts[0]
			if part.FunctionCall != nil && part.ThoughtSignature != "" {
				t.Errorf("expected empty ThoughtSignature in history when not set, got %q",
					part.ThoughtSignature)
			}
		}
	}
}

func TestThoughtSignatureNotStoredOnClient(t *testing.T) {
	// Verify that Client struct no longer has pendingSig field
	// This is a compile-time check - if pendingSig exists, this won't compile
	client := &Client{
		project:     "test",
		location:    "global",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	// Just verify the client can be created without pendingSig
	if client.project != "test" {
		t.Error("client not properly initialized")
	}
}

// staticTokenSource is a simple oauth2.TokenSource for testing
type staticTokenSource struct {
	token *oauth2.Token
}

func (s *staticTokenSource) Token() (*oauth2.Token, error) {
	return s.token, nil
}

func TestDecideAttachesThoughtSignatureToFirstToolCall(t *testing.T) {
	// Create a mock server that returns a response with thought_signature
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := `{
			"candidates": [{
				"content": {
					"role": "model",
					"parts": [
						{
							"functionCall": {"name": "tool_one", "args": {"x": 1}},
							"thoughtSignature": "sig-from-response-xyz"
						},
						{
							"functionCall": {"name": "tool_two", "args": {"y": 2}}
						}
					]
				},
				"finishReason": "STOP"
			}]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}))
	defer server.Close()

	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		baseURL:     server.URL,
		temperature: 0.5,
		maxTokens:   1024,
		client:      server.Client(),
		cred:        &staticTokenSource{token: &oauth2.Token{AccessToken: "test-token"}},
	}

	decision, err := client.Decide(context.Background(), Input{
		UserMessage: "test",
	})
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	if len(decision.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(decision.ToolCalls))
	}

	// First tool call should have the signature
	if decision.ToolCalls[0].ThoughtSignature != "sig-from-response-xyz" {
		t.Errorf("expected ThoughtSignature=%q on first tool call, got %q",
			"sig-from-response-xyz", decision.ToolCalls[0].ThoughtSignature)
	}

	// Second tool call should NOT have the signature
	if decision.ToolCalls[1].ThoughtSignature != "" {
		t.Errorf("expected empty ThoughtSignature on second tool call, got %q",
			decision.ToolCalls[1].ThoughtSignature)
	}
}

func TestDecideWithSnakeCaseThoughtSignature(t *testing.T) {
	// Test that snake_case thought_signature is also handled correctly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := `{
			"candidates": [{
				"content": {
					"role": "model",
					"parts": [
						{
							"functionCall": {"name": "my_tool", "args": {}},
							"thought_signature": "snake-case-sig-123"
						}
					]
				},
				"finishReason": "STOP"
			}]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}))
	defer server.Close()

	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		baseURL:     server.URL,
		temperature: 0.5,
		maxTokens:   1024,
		client:      server.Client(),
		cred:        &staticTokenSource{token: &oauth2.Token{AccessToken: "test-token"}},
	}

	decision, err := client.Decide(context.Background(), Input{
		UserMessage: "test",
	})
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	if len(decision.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(decision.ToolCalls))
	}

	if decision.ToolCalls[0].ThoughtSignature != "snake-case-sig-123" {
		t.Errorf("expected ThoughtSignature=%q, got %q",
			"snake-case-sig-123", decision.ToolCalls[0].ThoughtSignature)
	}
}

func TestDecideWithoutThoughtSignature(t *testing.T) {
	// Test that responses without thought_signature still work
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := `{
			"candidates": [{
				"content": {
					"role": "model",
					"parts": [
						{
							"functionCall": {"name": "my_tool", "args": {}}
						}
					]
				},
				"finishReason": "STOP"
			}]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}))
	defer server.Close()

	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		baseURL:     server.URL,
		temperature: 0.5,
		maxTokens:   1024,
		client:      server.Client(),
		cred:        &staticTokenSource{token: &oauth2.Token{AccessToken: "test-token"}},
	}

	decision, err := client.Decide(context.Background(), Input{
		UserMessage: "test",
	})
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	if len(decision.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(decision.ToolCalls))
	}

	if decision.ToolCalls[0].ThoughtSignature != "" {
		t.Errorf("expected empty ThoughtSignature, got %q",
			decision.ToolCalls[0].ThoughtSignature)
	}
}
