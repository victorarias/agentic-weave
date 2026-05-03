package vertex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
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

func TestBuildRequestIncludesThoughtSignatureFromHistory(t *testing.T) {
	// History is the canonical source for tool calls. This test verifies that
	// ThoughtSignature is preserved when tool calls come from History.
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	input := Input{
		UserMessage: "continue after tools",
		History: []message.AgentMessage{
			{
				Role:    message.RoleUser,
				Content: "test message",
			},
			{
				Role: message.RoleAssistant,
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
			},
			{
				Role: message.RoleTool,
				ToolResults: []agentic.ToolResult{
					{ID: "call-0", Name: "test_tool", Output: json.RawMessage(`{"result":"ok"}`)},
				},
			},
			{
				Role: message.RoleTool,
				ToolResults: []agentic.ToolResult{
					{ID: "call-1", Name: "another_tool", Output: json.RawMessage(`{"result":"done"}`)},
				},
			},
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

func TestAppendHistoryIncludesThoughtSignature(t *testing.T) {
	history := []message.AgentMessage{
		{
			Role:    message.RoleUser,
			Content: "first message",
		},
		{
			Role: message.RoleAssistant,
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
		},
		{
			Role: message.RoleTool,
			ToolResults: []agentic.ToolResult{
				{ID: "hist-call-0", Name: "historical_tool", Output: json.RawMessage(`{"y":2}`)},
			},
		},
		{
			Role: message.RoleTool,
			ToolResults: []agentic.ToolResult{
				{ID: "hist-call-1", Name: "another_hist_tool", Output: json.RawMessage(`{}`)},
			},
		},
		{
			Role:    message.RoleAssistant,
			Content: "done",
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
	history := []message.AgentMessage{
		{
			Role:    message.RoleUser,
			Content: "message without signature",
		},
		{
			Role: message.RoleAssistant,
			ToolCalls: []agentic.ToolCall{
				{
					ID:    "no-sig-call",
					Name:  "tool_without_sig",
					Input: json.RawMessage(`{}`),
					// No ThoughtSignature
				},
			},
		},
		{
			Role: message.RoleTool,
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

// staticTokenSource is a simple oauth2.TokenSource for testing
type staticTokenSource struct {
	token *oauth2.Token
}

func (s *staticTokenSource) Token() (*oauth2.Token, error) {
	return s.token, nil
}

func TestDecideCapturesThoughtSignatureFromEachPart(t *testing.T) {
	// Per Vertex AI docs: for parallel function calls, only the first
	// functionCall part contains the thought_signature.
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

func TestAppendHistoryWithAgentMessage(t *testing.T) {
	history := []message.AgentMessage{
		{
			Role:    message.RoleUser,
			Content: "Hello",
		},
		{
			Role:    message.RoleAssistant,
			Content: "Hi there!",
		},
		{
			Role: message.RoleAssistant,
			ToolCalls: []agentic.ToolCall{
				{ID: "tc1", Name: "search", Input: json.RawMessage(`{"q":"test"}`)},
			},
		},
		{
			Role: message.RoleTool,
			ToolResults: []agentic.ToolResult{
				{ID: "tc1", Name: "search", Output: json.RawMessage(`"results"`)},
			},
		},
		{
			Role:    message.RoleSystem,
			Content: "Context summary from compaction",
		},
	}

	contents := appendHistory(nil, history)

	// Check expected structure
	if len(contents) != 5 {
		t.Fatalf("expected 5 contents, got %d", len(contents))
	}

	// User message
	if contents[0].Role != "user" || contents[0].Parts[0].Text != "Hello" {
		t.Errorf("expected user message, got %+v", contents[0])
	}

	// Assistant text reply
	if contents[1].Role != "model" || contents[1].Parts[0].Text != "Hi there!" {
		t.Errorf("expected model text, got %+v", contents[1])
	}

	// Assistant tool call
	if contents[2].Role != "model" || contents[2].Parts[0].FunctionCall == nil {
		t.Errorf("expected model function call, got %+v", contents[2])
	}

	// Tool result
	if contents[3].Role != "user" || contents[3].Parts[0].FunctionResponse == nil {
		t.Errorf("expected user function response, got %+v", contents[3])
	}

	// System summary (as user message with prefix)
	if contents[4].Role != "user" || !strings.Contains(contents[4].Parts[0].Text, "[Context Summary]") {
		t.Errorf("expected system summary as user message, got %+v", contents[4])
	}
}

func TestAppendHistoryPreservesAssistantTextWithToolCalls(t *testing.T) {
	history := []message.AgentMessage{
		{
			Role:    message.RoleAssistant,
			Content: "Hello",
			ToolCalls: []agentic.ToolCall{
				{ID: "tc1", Name: "search", Input: json.RawMessage(`{"q":"test"}`)},
			},
		},
	}

	contents := appendHistory(nil, history)

	hasText := false
	hasCall := false
	for _, content := range contents {
		for _, part := range content.Parts {
			if part.Text == "Hello" {
				hasText = true
			}
			if part.FunctionCall != nil && part.FunctionCall.Name == "search" {
				hasCall = true
			}
		}
	}

	if !hasText || !hasCall {
		t.Fatalf("expected both assistant text and tool call, got text=%v call=%v", hasText, hasCall)
	}
}

func TestBuildRequestHistorySerializesToolCallsOnce(t *testing.T) {
	// History is the canonical source for tool calls. Verify tool calls from history
	// are serialized exactly once in the request.
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	input := Input{
		UserMessage: "continue",
		History: []message.AgentMessage{
			{
				Role:    message.RoleUser,
				Content: "call a tool",
			},
			{
				Role: message.RoleAssistant,
				ToolCalls: []agentic.ToolCall{
					{
						ID:               "call-0",
						Name:             "my_tool",
						Input:            json.RawMessage(`{"arg":"value"}`),
						ThoughtSignature: "sig-123",
					},
				},
			},
			{
				Role: message.RoleTool,
				ToolResults: []agentic.ToolResult{
					{ID: "call-0", Name: "my_tool", Output: json.RawMessage(`{"result":"done"}`)},
				},
			},
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

	// Count how many times my_tool functionCall appears
	functionCallCount := 0
	functionResponseCount := 0
	for _, content := range req.Contents {
		for _, part := range content.Parts {
			if part.FunctionCall != nil && part.FunctionCall.Name == "my_tool" {
				functionCallCount++
			}
			if part.FunctionResponse != nil && part.FunctionResponse.Name == "my_tool" {
				functionResponseCount++
			}
		}
	}

	if functionCallCount != 1 {
		t.Errorf("expected exactly 1 functionCall for my_tool, got %d", functionCallCount)
	}
	if functionResponseCount != 1 {
		t.Errorf("expected exactly 1 functionResponse for my_tool, got %d", functionResponseCount)
	}
}

func TestBuildRequestSkipsUserMessageAfterToolResult(t *testing.T) {
	// When history ends with a tool result and UserMessage is empty,
	// buildRequest should NOT add an extra user message. This allows
	// the model to resume thinking directly after the function response.
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	input := Input{
		UserMessage: "", // Empty - model should resume after tool result
		History: []message.AgentMessage{
			{
				Role:    message.RoleUser,
				Content: "call a tool",
			},
			{
				Role: message.RoleAssistant,
				ToolCalls: []agentic.ToolCall{
					{
						ID:               "call-0",
						Name:             "my_tool",
						Input:            json.RawMessage(`{"arg":"value"}`),
						ThoughtSignature: "sig-123",
					},
				},
			},
			{
				Role: message.RoleTool,
				ToolResults: []agentic.ToolResult{
					{ID: "call-0", Name: "my_tool", Output: json.RawMessage(`{"result":"done"}`)},
				},
			},
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

	// Should have exactly 3 contents: user, model(functionCall), user(functionResponse)
	// NO extra user message at the end
	if len(req.Contents) != 3 {
		t.Errorf("expected 3 contents (no extra user message), got %d", len(req.Contents))
		for i, c := range req.Contents {
			t.Logf("  content[%d]: role=%s, parts=%d", i, c.Role, len(c.Parts))
		}
	}

	// Last content should be the function response, not a text message
	lastContent := req.Contents[len(req.Contents)-1]
	if lastContent.Role != "user" {
		t.Errorf("expected last content role to be 'user', got %q", lastContent.Role)
	}
	if lastContent.Parts[0].FunctionResponse == nil {
		t.Error("expected last content to be a function response, not text")
	}
}

func TestBuildRequestIncludesGoogleSearch(t *testing.T) {
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	input := Input{
		UserMessage:  "what happened today?",
		GoogleSearch: true,
		Tools: []agentic.ToolDefinition{
			{Name: "my_tool", Description: "a tool"},
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

	if len(req.Tools) != 2 {
		t.Fatalf("expected 2 tool entries (functions + googleSearch), got %d", len(req.Tools))
	}

	// First entry: function declarations
	if len(req.Tools[0].FunctionDeclarations) != 1 {
		t.Errorf("expected 1 function declaration, got %d", len(req.Tools[0].FunctionDeclarations))
	}
	if req.Tools[0].GoogleSearch != nil {
		t.Error("first tool entry should not have googleSearch")
	}

	// Second entry: google search
	if req.Tools[1].GoogleSearch == nil {
		t.Error("second tool entry should have googleSearch")
	}
	if len(req.Tools[1].FunctionDeclarations) != 0 {
		t.Error("second tool entry should not have function declarations")
	}
}

func TestBuildRequestOmitsGoogleSearchWhenDisabled(t *testing.T) {
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	input := Input{
		UserMessage: "hello",
		Tools: []agentic.ToolDefinition{
			{Name: "my_tool", Description: "a tool"},
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

	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool entry, got %d", len(req.Tools))
	}
	if req.Tools[0].GoogleSearch != nil {
		t.Error("should not have googleSearch when disabled")
	}
}

func TestBuildRequestGoogleSearchOnlyNoFunctions(t *testing.T) {
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	input := Input{
		UserMessage:  "search the web",
		GoogleSearch: true,
	}

	reqBody, err := client.buildRequest(input)
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}

	var req vertexRequest
	if err := json.Unmarshal(reqBody, &req); err != nil {
		t.Fatalf("failed to parse request: %v", err)
	}

	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool entry (googleSearch only), got %d", len(req.Tools))
	}
	if req.Tools[0].GoogleSearch == nil {
		t.Error("expected googleSearch tool")
	}
}

func TestBuildRequestAddsUserMessageWhenProvided(t *testing.T) {
	// When UserMessage is provided, it should always be added
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	input := Input{
		UserMessage: "what next?", // Explicit user message
		History: []message.AgentMessage{
			{
				Role:    message.RoleUser,
				Content: "call a tool",
			},
			{
				Role: message.RoleAssistant,
				ToolCalls: []agentic.ToolCall{
					{
						ID:    "call-0",
						Name:  "my_tool",
						Input: json.RawMessage(`{}`),
					},
				},
			},
			{
				Role: message.RoleTool,
				ToolResults: []agentic.ToolResult{
					{ID: "call-0", Name: "my_tool", Output: json.RawMessage(`{}`)},
				},
			},
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

	// Should have 4 contents: user, model, user(functionResponse), user("what next?")
	if len(req.Contents) != 4 {
		t.Errorf("expected 4 contents (with user message), got %d", len(req.Contents))
	}

	// Last content should be the user's new message
	lastContent := req.Contents[len(req.Contents)-1]
	if lastContent.Role != "user" || lastContent.Parts[0].Text != "what next?" {
		t.Errorf("expected last content to be user text 'what next?', got role=%q text=%q",
			lastContent.Role, lastContent.Parts[0].Text)
	}
}

func TestBuildRequestIncludesUserInlineData(t *testing.T) {
	imageBytes := []byte("fake-png-data")
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	reqBody, err := client.buildRequest(Input{
		UserMessage: "describe this",
		UserInlineData: []agentic.InlineData{
			{MIMEType: "image/png", Data: imageBytes},
		},
	})
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}

	var req vertexRequest
	if err := json.Unmarshal(reqBody, &req); err != nil {
		t.Fatalf("failed to parse request: %v", err)
	}
	if len(req.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(req.Contents))
	}
	content := req.Contents[0]
	if content.Role != "user" {
		t.Fatalf("expected user role, got %q", content.Role)
	}
	if len(content.Parts) != 2 {
		t.Fatalf("expected text + inlineData parts, got %#v", content.Parts)
	}
	if content.Parts[0].Text != "describe this" {
		t.Fatalf("unexpected text part: %#v", content.Parts[0])
	}
	inlinePart := content.Parts[1]
	if inlinePart.InlineData == nil {
		t.Fatal("expected inline data part")
	}
	if inlinePart.InlineData.MIMEType != "image/png" {
		t.Fatalf("expected image/png MIME type, got %q", inlinePart.InlineData.MIMEType)
	}
	decoded, err := base64.StdEncoding.DecodeString(inlinePart.InlineData.Data)
	if err != nil {
		t.Fatalf("decode inline data: %v", err)
	}
	if string(decoded) != string(imageBytes) {
		t.Fatalf("decoded image data mismatch: got %q want %q", decoded, imageBytes)
	}
}

func TestAppendHistoryToolResultWithInlineData(t *testing.T) {
	imageBytes := []byte("fake-png-data")
	history := []message.AgentMessage{
		{
			Role: message.RoleAssistant,
			ToolCalls: []agentic.ToolCall{
				{ID: "call-img", Name: "evaluate_image", Input: json.RawMessage(`{"ref":"img-1"}`)},
			},
		},
		{
			Role: message.RoleTool,
			ToolResults: []agentic.ToolResult{
				{
					ID:     "call-img",
					Name:   "evaluate_image",
					Output: json.RawMessage(`"Here is the image for evaluation"`),
					InlineData: []agentic.InlineData{
						{MIMEType: "image/png", Data: imageBytes},
					},
				},
			},
		},
	}

	contents := appendHistory(nil, history)

	// Find the tool result content
	var toolContent *vertexContent
	for i := range contents {
		if contents[i].Role == "user" && len(contents[i].Parts) > 0 && contents[i].Parts[0].FunctionResponse != nil {
			toolContent = &contents[i]
			break
		}
	}
	if toolContent == nil {
		t.Fatal("expected a user content with function response")
	}

	if len(toolContent.Parts) != 2 {
		t.Fatalf("expected 2 parts (functionResponse + inlineData), got %d", len(toolContent.Parts))
	}

	// First part: function response
	if toolContent.Parts[0].FunctionResponse == nil {
		t.Error("first part should be a function response")
	}

	// Second part: inline data with base64-encoded image
	inlinePart := toolContent.Parts[1]
	if inlinePart.InlineData == nil {
		t.Fatal("second part should have inline data")
	}
	if inlinePart.InlineData.MIMEType != "image/png" {
		t.Errorf("expected mimeType=image/png, got %q", inlinePart.InlineData.MIMEType)
	}
	decoded, err := base64.StdEncoding.DecodeString(inlinePart.InlineData.Data)
	if err != nil {
		t.Fatalf("failed to decode base64 data: %v", err)
	}
	if string(decoded) != string(imageBytes) {
		t.Errorf("decoded image data mismatch: got %q, want %q", decoded, imageBytes)
	}
}

func TestBuildRequestIncludesConfigLabels(t *testing.T) {
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
		labels:      map[string]string{"service": "conductor-bot", "team": "drm"},
	}

	reqBody, err := client.buildRequest(Input{UserMessage: "hello"})
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}

	var req vertexRequest
	if err := json.Unmarshal(reqBody, &req); err != nil {
		t.Fatalf("failed to parse request: %v", err)
	}

	if len(req.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(req.Labels))
	}
	if req.Labels["service"] != "conductor-bot" {
		t.Errorf("expected service=conductor-bot, got %q", req.Labels["service"])
	}
	if req.Labels["team"] != "drm" {
		t.Errorf("expected team=drm, got %q", req.Labels["team"])
	}
}

func TestBuildRequestMergesInputLabelsOverConfigLabels(t *testing.T) {
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
		labels:      map[string]string{"service": "default", "team": "drm"},
	}

	input := Input{
		UserMessage: "hello",
		Labels:      map[string]string{"service": "conductor-bot", "env": "prod"},
	}

	reqBody, err := client.buildRequest(input)
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}

	var req vertexRequest
	if err := json.Unmarshal(reqBody, &req); err != nil {
		t.Fatalf("failed to parse request: %v", err)
	}

	if len(req.Labels) != 3 {
		t.Fatalf("expected 3 labels, got %d: %v", len(req.Labels), req.Labels)
	}
	if req.Labels["service"] != "conductor-bot" {
		t.Errorf("per-request label should override config: got service=%q", req.Labels["service"])
	}
	if req.Labels["team"] != "drm" {
		t.Errorf("config label should be preserved: got team=%q", req.Labels["team"])
	}
	if req.Labels["env"] != "prod" {
		t.Errorf("per-request label should be included: got env=%q", req.Labels["env"])
	}
}

func TestBuildRequestOmitsLabelsWhenEmpty(t *testing.T) {
	client := &Client{
		project:     "test-project",
		location:    "us-central1",
		model:       "gemini-pro",
		temperature: 0.5,
		maxTokens:   1024,
	}

	reqBody, err := client.buildRequest(Input{UserMessage: "hello"})
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}

	// Verify "labels" key is not present in JSON (omitempty)
	if strings.Contains(string(reqBody), `"labels"`) {
		t.Error("expected labels to be omitted from JSON when empty")
	}
}

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "single pair",
			input:    "service=conductor-bot",
			expected: map[string]string{"service": "conductor-bot"},
		},
		{
			name:     "multiple pairs",
			input:    "service=conductor-bot,team=drm,env=prod",
			expected: map[string]string{"service": "conductor-bot", "team": "drm", "env": "prod"},
		},
		{
			name:     "with spaces",
			input:    " service = conductor-bot , team = drm ",
			expected: map[string]string{"service": "conductor-bot", "team": "drm"},
		},
		{
			name:     "empty value",
			input:    "service=",
			expected: map[string]string{"service": ""},
		},
		{
			name:     "trailing comma",
			input:    "service=conductor-bot,",
			expected: map[string]string{"service": "conductor-bot"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: map[string]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLabels(tc.input)
			if len(got) != len(tc.expected) {
				t.Fatalf("expected %d labels, got %d: %v", len(tc.expected), len(got), got)
			}
			for k, want := range tc.expected {
				if got[k] != want {
					t.Errorf("label %q: expected %q, got %q", k, want, got[k])
				}
			}
		})
	}
}

func TestConfigFromEnvParsesLabels(t *testing.T) {
	t.Setenv("VERTEX_PROJECT", "")
	t.Setenv("VERTEX_MODEL", "")
	t.Setenv("VERTEX_LOCATION", "")
	t.Setenv("VERTEX_API_BASE", "")
	t.Setenv("VERTEX_AI_API_KEY", "")
	t.Setenv("VERTEX_TEMPERATURE", "")
	t.Setenv("VERTEX_MAX_TOKENS", "")
	t.Setenv("VERTEX_LABELS", "service=conductor-bot,team=drm")

	cfg := ConfigFromEnv()
	if len(cfg.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d: %v", len(cfg.Labels), cfg.Labels)
	}
	if cfg.Labels["service"] != "conductor-bot" {
		t.Errorf("expected service=conductor-bot, got %q", cfg.Labels["service"])
	}
	if cfg.Labels["team"] != "drm" {
		t.Errorf("expected team=drm, got %q", cfg.Labels["team"])
	}
}

func TestAppendHistoryToolResultWithoutInlineData(t *testing.T) {
	// Verify that tool results without inline data still produce a single-part response
	history := []message.AgentMessage{
		{
			Role: message.RoleAssistant,
			ToolCalls: []agentic.ToolCall{
				{ID: "call-text", Name: "search", Input: json.RawMessage(`{}`)},
			},
		},
		{
			Role: message.RoleTool,
			ToolResults: []agentic.ToolResult{
				{ID: "call-text", Name: "search", Output: json.RawMessage(`"found 3 results"`)},
			},
		},
	}

	contents := appendHistory(nil, history)

	var toolContent *vertexContent
	for i := range contents {
		if contents[i].Role == "user" && len(contents[i].Parts) > 0 && contents[i].Parts[0].FunctionResponse != nil {
			toolContent = &contents[i]
			break
		}
	}
	if toolContent == nil {
		t.Fatal("expected a user content with function response")
	}

	if len(toolContent.Parts) != 1 {
		t.Errorf("expected 1 part (functionResponse only), got %d", len(toolContent.Parts))
	}
}
