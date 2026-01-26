package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/joho/godotenv"
	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	anthropic "github.com/victorarias/agentic-weave/agentic/providers/anthropic"
)

func init() {
	dir, _ := os.Getwd()
	for {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}

// TestAnthropicE2E tests the complete agentic loop:
// user message → tool call → tool result → final answer
//
// Uses 2 API calls total.
func TestAnthropicE2E(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	model := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL"))
	if model == "" {
		model = string(sdk.ModelClaudeSonnet4_5_20250929)
	}

	client, err := anthropic.New(anthropic.Config{
		APIKey: apiKey,
		Model:  model,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tools := []agentic.ToolDefinition{{
		Name:        "add",
		Description: "Add two numbers",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`),
	}}

	// Call 1: User asks question → model returns tool call
	decision, err := client.Decide(ctx, anthropic.Input{
		SystemPrompt: "Use the add tool for math. After getting the result, state the answer.",
		UserMessage:  "What is 42 + 17?",
		Tools:        tools,
		MaxTokens:    256,
	})
	if err != nil {
		t.Fatalf("call 1 failed: %v", err)
	}
	if len(decision.ToolCalls) == 0 {
		t.Fatalf("expected tool call, got: %s", decision.Reply)
	}

	tc := decision.ToolCalls[0]
	if tc.Name != "add" {
		t.Fatalf("expected 'add' tool call, got: %s", tc.Name)
	}

	// Execute tool
	var args struct{ A, B float64 }
	_ = json.Unmarshal(tc.Input, &args)
	result, _ := json.Marshal(map[string]float64{"sum": args.A + args.B})

	// Call 2: Send tool result → model returns final answer
	decision, err = client.Decide(ctx, anthropic.Input{
		SystemPrompt: "Use the add tool for math. After getting the result, state the answer.",
		Tools:        tools,
		MaxTokens:    256,
		History: []message.AgentMessage{
			{Role: message.RoleUser, Content: "What is 42 + 17?"},
			{Role: message.RoleAssistant, ToolCalls: []agentic.ToolCall{tc}},
			{Role: message.RoleTool, ToolResults: []agentic.ToolResult{{ID: tc.ID, Name: tc.Name, Output: result}}},
		},
	})
	if err != nil {
		t.Fatalf("call 2 failed: %v", err)
	}
	if !strings.Contains(decision.Reply, "59") {
		t.Fatalf("expected '59' in reply, got: %s", decision.Reply)
	}
}
