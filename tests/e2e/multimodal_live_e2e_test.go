package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/providers"
	anthropic "github.com/victorarias/agentic-weave/agentic/providers/anthropic"
	openai "github.com/victorarias/agentic-weave/agentic/providers/openai"
	"github.com/victorarias/agentic-weave/agentic/providers/vertex"
)

func TestAnthropicVisionUserInlineDataLive(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	model := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL"))
	if model == "" {
		model = string(sdk.ModelClaudeSonnet4_5_20250929)
	}

	client, err := anthropic.New(anthropic.Config{
		APIKey:    apiKey,
		Model:     model,
		MaxTokens: 128,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	decision, err := providers.Decide(ctx, anthropic.NewStreamerAdapter(client), visionInput())
	if err != nil {
		t.Fatalf("live vision call failed: %v", err)
	}
	assertSawRedBlue(t, "anthropic", decision.Reply)
}

func TestOpenAIVisionUserInlineDataLive(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	model := strings.TrimSpace(os.Getenv("OPENAI_E2E_VISION_MODEL"))
	if model == "" {
		model = "gpt-4o-mini"
	}

	client, err := openai.New(openai.Config{
		APIKey:         apiKey,
		Model:          model,
		MaxTokens:      128,
		MaxTokensField: openai.MaxTokensFieldLegacy,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	decision, err := providers.Decide(ctx, client, visionInput())
	if err != nil {
		t.Fatalf("live vision call failed: %v", err)
	}
	assertSawRedBlue(t, "openai", decision.Reply)
}

func TestOpenRouterVisionUserInlineDataLive(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}
	model := strings.TrimSpace(os.Getenv("OPENROUTER_E2E_VISION_MODEL"))
	if model == "" {
		model = "openai/gpt-4o-mini"
	}

	client, err := openai.New(openai.Config{
		APIKey:         apiKey,
		Model:          model,
		BaseURL:        "https://openrouter.ai/api/v1",
		MaxTokens:      128,
		MaxTokensField: openai.MaxTokensFieldLegacy,
		Headers: map[string][]string{
			"HTTP-Referer": {"https://github.com/victorarias/agentic-weave"},
			"X-Title":      {"agentic-weave-vision-e2e"},
		},
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	decision, err := providers.Decide(ctx, client, visionInput())
	if err != nil {
		t.Fatalf("live vision call failed: %v", err)
	}
	assertSawRedBlue(t, "openrouter", decision.Reply)
}

func TestVertexVisionUserInlineDataLive(t *testing.T) {
	apiKey := os.Getenv("VERTEX_AI_API_KEY")
	if apiKey == "" {
		t.Skip("VERTEX_AI_API_KEY not set")
	}
	model := strings.TrimSpace(os.Getenv("VERTEX_MODEL"))
	if model == "" {
		model = "gemini-2.5-flash"
	}

	client, err := vertex.New(vertex.Config{
		APIKey:    apiKey,
		Model:     model,
		MaxTokens: 128,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	decision, err := client.Decide(ctx, vertex.Input{
		SystemPrompt: visionInput().SystemPrompt,
		UserMessage:  visionInput().UserMessage,
		UserInlineData: []agentic.InlineData{
			{MIMEType: "image/png", Data: makeTestImage()},
		},
	})
	if err != nil {
		t.Fatalf("live vision call failed: %v", err)
	}
	assertSawRedBlue(t, "vertex", decision.Reply)
}

func visionInput() providers.Input {
	return providers.Input{
		SystemPrompt: "You are a precise vision test assistant. Answer with the colors you see, not guesses about the test.",
		UserMessage:  "This image is a simple two-color test pattern. What color is on the left half and what color is on the right half? Answer concisely.",
		UserInlineData: []agentic.InlineData{
			{MIMEType: "image/png", Data: makeTestImage()},
		},
		MaxTokens: 128,
	}
}

func assertSawRedBlue(t *testing.T, provider string, reply string) {
	t.Helper()
	t.Logf("%s live vision reply: %s", provider, reply)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "red") || !strings.Contains(lower, "blue") {
		t.Fatalf("expected %s reply to mention red and blue, got: %s", provider, reply)
	}
}
