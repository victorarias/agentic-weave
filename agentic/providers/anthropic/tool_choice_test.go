package anthropic

import (
	"testing"

	anthropicapi "github.com/anthropics/anthropic-sdk-go"
)

func TestApplyToolChoice(t *testing.T) {
	t.Parallel()

	t.Run("nil choice keeps default", func(t *testing.T) {
		req := anthropicapi.MessageNewParams{}
		if err := applyToolChoice(&req, nil); err != nil {
			t.Fatalf("applyToolChoice returned error: %v", err)
		}
	})

	t.Run("mode tool requires name", func(t *testing.T) {
		req := anthropicapi.MessageNewParams{}
		err := applyToolChoice(&req, &ToolChoice{Mode: "tool"})
		if err == nil {
			t.Fatalf("expected error for missing tool name")
		}
	})

	t.Run("mode tool sets tool choice", func(t *testing.T) {
		req := anthropicapi.MessageNewParams{}
		err := applyToolChoice(&req, &ToolChoice{Mode: "tool", Name: "report"})
		if err != nil {
			t.Fatalf("applyToolChoice returned error: %v", err)
		}
		if req.ToolChoice.OfTool == nil {
			t.Fatalf("expected tool choice to be set")
		}
		if req.ToolChoice.OfTool.Name != "report" {
			t.Fatalf("expected tool name report, got %q", req.ToolChoice.OfTool.Name)
		}
	})
}
