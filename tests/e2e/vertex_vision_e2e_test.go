package e2e

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/providers/vertex"
)

// makeTestImage creates a PNG with a red left half and blue right half.
func makeTestImage() []byte {
	const w, h = 200, 100
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x < w/2 {
				img.Set(x, y, color.RGBA{R: 255, A: 255}) // red
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255}) // blue
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// TestVertexVisionUserInlineData sends a synthetic image as UserInlineData
// on the current user message and verifies the model can describe it.
func TestVertexVisionUserInlineData(t *testing.T) {
	apiKey := os.Getenv("VERTEX_AI_API_KEY")
	if apiKey == "" {
		t.Skip("VERTEX_AI_API_KEY not set")
	}

	client, err := vertex.New(vertex.Config{
		APIKey: apiKey,
		Model:  "gemini-2.5-flash",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	imgData := makeTestImage()

	decision, err := client.Decide(ctx, vertex.Input{
		SystemPrompt: "You are a vision assistant. Describe images concisely.",
		UserMessage:  "What colors do you see in this image?",
		UserInlineData: []agentic.InlineData{
			{MIMEType: "image/png", Data: imgData},
		},
	})
	if err != nil {
		t.Fatalf("Decide failed: %v", err)
	}

	reply := strings.ToLower(decision.Reply)
	t.Logf("Model reply: %s", decision.Reply)

	if !strings.Contains(reply, "red") && !strings.Contains(reply, "blue") {
		t.Errorf("expected reply to mention red or blue, got: %s", decision.Reply)
	}
}

// TestVertexVisionInlineDataInHistory sends an image in history (as if from
// a previous turn's user message) and verifies the model can still reference it.
func TestVertexVisionInlineDataInHistory(t *testing.T) {
	apiKey := os.Getenv("VERTEX_AI_API_KEY")
	if apiKey == "" {
		t.Skip("VERTEX_AI_API_KEY not set")
	}

	client, err := vertex.New(vertex.Config{
		APIKey: apiKey,
		Model:  "gemini-2.5-flash",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	imgData := makeTestImage()

	// Simulate: user sent image on turn 1, now asking a follow-up on turn 2.
	decision, err := client.Decide(ctx, vertex.Input{
		SystemPrompt: "You are a vision assistant. Be concise.",
		UserMessage:  "Which color is on the left side of the image I sent earlier?",
		History: []message.AgentMessage{
			{
				Role:    message.RoleUser,
				Content: "Here's an image to analyze",
				InlineData: []agentic.InlineData{
					{MIMEType: "image/png", Data: imgData},
				},
			},
			{
				Role:    message.RoleAssistant,
				Content: "I see a two-toned image. I'll analyze it further if you have questions.",
			},
		},
	})
	if err != nil {
		t.Fatalf("Decide failed: %v", err)
	}

	reply := strings.ToLower(decision.Reply)
	t.Logf("Model reply: %s", decision.Reply)

	if !strings.Contains(reply, "red") {
		t.Errorf("expected reply to mention 'red' (left side), got: %s", decision.Reply)
	}
}
