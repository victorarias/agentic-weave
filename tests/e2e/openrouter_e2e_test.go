package e2e

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/providers"
	openai "github.com/victorarias/agentic-weave/agentic/providers/openai"
)

// TestOpenRouterE2E proves the OpenAI-completions adapter reaches OpenRouter
// end-to-end with the new extension fields wired up: provider routing with
// require_parameters=true (and no `order`, see INV-2 in huxie's plan), the
// HTTP-Referer / X-Title attribution headers, and a tool round-trip.
//
// Gated on OPENROUTER_API_KEY + OPENROUTER_E2E_MODEL (any cheap or free model
// id will do). Skips otherwise so the CI default stays mock-only.
func TestOpenRouterE2E(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}
	model := strings.TrimSpace(os.Getenv("OPENROUTER_E2E_MODEL"))
	if model == "" {
		t.Skip("OPENROUTER_E2E_MODEL not set")
	}

	requireParams := true
	client, err := openai.New(openai.Config{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: "https://openrouter.ai/api/v1",
		ProviderRouting: &openai.ProviderRouting{
			RequireParameters: &requireParams,
		},
		Headers: map[string][]string{
			"HTTP-Referer": {"https://github.com/victorarias/agentic-weave"},
			"X-Title":      {"agentic-weave-e2e"},
		},
		MaxTokens:      256,
		MaxTokensField: openai.MaxTokensFieldLegacy,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tools := []agentic.ToolDefinition{{
		Name:        "add",
		Description: "Add two numbers",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`),
	}}

	first, err := providers.Decide(ctx, client, providers.Input{
		SystemPrompt: "Use the add tool for math. Reply with the answer afterwards.",
		UserMessage:  "What is 42 + 17?",
		Tools:        tools,
	})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(first.ToolCalls) == 0 {
		t.Fatalf("expected tool call, got reply=%q", first.Reply)
	}

	tc := first.ToolCalls[0]
	var args struct{ A, B float64 }
	_ = json.Unmarshal(tc.Input, &args)
	result, _ := json.Marshal(map[string]float64{"sum": args.A + args.B})

	second, err := providers.Decide(ctx, client, providers.Input{
		SystemPrompt: "Use the add tool for math. Reply with the answer afterwards.",
		Tools:        tools,
		History: []message.AgentMessage{
			{Role: message.RoleUser, Content: "What is 42 + 17?"},
			{Role: message.RoleAssistant, ToolCalls: []agentic.ToolCall{tc}},
			{Role: message.RoleTool, ToolResults: []agentic.ToolResult{{ID: tc.ID, Name: tc.Name, Output: result}}},
		},
	})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !strings.Contains(second.Reply, "59") {
		t.Fatalf("expected '59' in reply, got: %s", second.Reply)
	}
}
