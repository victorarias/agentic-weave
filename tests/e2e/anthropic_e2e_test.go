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
	"github.com/victorarias/agentic-weave/agentic/usage"
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

// TestAnthropicStreamingE2E validates streaming behavior:
// - text deltas stream for a non-tool request
// - tool calls stream for a tool request
// - done events include stop reason + usage
func TestAnthropicStreamingE2E(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Step 1: Streaming text without tools.
	textStream, err := client.Stream(ctx, anthropic.Input{
		SystemPrompt: "Respond with the single word OK.",
		UserMessage:  "Say OK.",
		MaxTokens:    16,
	})
	if err != nil {
		t.Fatalf("text stream failed: %v", err)
	}
	_, textCounts := collectStream(t, textStream)
	if textCounts.TextDeltas == 0 {
		t.Fatalf("expected text deltas in streaming text response")
	}
	if !textCounts.DoneSeen || textCounts.StopReason == "" || !textCounts.UsageSeen {
		t.Fatalf("expected done event with stop reason + usage in text response")
	}

	// Step 2: Streaming tool-use flow.
	tools := []agentic.ToolDefinition{{
		Name:        "add",
		Description: "Add two numbers",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`),
	}}

	var (
		userQuery = "What is 42+17? Answer with only the final number. You must use the add tool."
		system    = "Do not answer before the tool result. If the tool result is an error, explain the error and do not call tools again."
		followup  = "Answer with only the final number."
	)

	var result toolRunResult
	for attempt := 0; attempt < 2; attempt++ {
		result = runToolStreaming(ctx, t, client, tools, system, followup, userQuery)
		if result.AnswerSeen {
			break
		}
		t.Logf("retrying tool streaming flow (attempt %d)", attempt+2)
	}

	summary, err := json.MarshalIndent(map[string]any{
		"reply":                 result.Reply,
		"answer_seen":           result.AnswerSeen,
		"tool_seen":             result.ToolSeen,
		"done_seen":             result.DoneSeen,
		"usage_seen":            result.UsageSeen,
		"stop_seen":             result.StopSeen,
		"tool_errors":           result.ToolErrors,
		"post_error_tool_calls": result.PostErrorToolCalls,
		"post_error_reply":      result.PostErrorReply,
	}, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal summary: %v", err)
	}
	t.Logf("final_output:\n%s", string(summary))

	if !result.ToolSeen {
		t.Fatalf("expected at least one tool call during streaming tool flow")
	}
	if !result.DoneSeen || !result.UsageSeen {
		t.Fatalf("expected done event with usage during tool flow")
	}
	if !result.StopSeen {
		t.Fatalf("expected stop reason during tool flow")
	}
	if !result.AnswerSeen {
		if len(result.ToolErrors) > 0 && result.PostErrorToolCalls > 0 {
			t.Fatalf("expected '59' in reply, got: %s (tool errors: %s; model called tools after error: %d; post-error reply: %q)",
				result.Reply,
				strings.Join(result.ToolErrors, " | "),
				result.PostErrorToolCalls,
				result.PostErrorReply,
			)
		}
		if len(result.ToolErrors) > 0 {
			t.Fatalf("expected '59' in reply, got: %s (tool errors: %s)", result.Reply, strings.Join(result.ToolErrors, " | "))
		}
		t.Fatalf("expected '59' in reply, got: %s", result.Reply)
	}
}

type streamCounts struct {
	TextDeltas   int
	ToolCalls    int
	ToolCallList []agentic.ToolCall
	DoneSeen     bool
	StopReason   usage.StopReason
	UsageSeen    bool
}

func collectStream(t *testing.T, stream <-chan anthropic.StreamEvent) (string, streamCounts) {
	t.Helper()

	var reply strings.Builder
	counts := streamCounts{}

	for event := range stream {
		switch e := event.(type) {
		case anthropic.TextDeltaEvent:
			reply.WriteString(e.Delta)
			if strings.TrimSpace(e.Delta) != "" {
				counts.TextDeltas++
			}
		case anthropic.ToolCallEvent:
			counts.ToolCalls++
			counts.ToolCallList = append(counts.ToolCallList, e.Call)
		case anthropic.DoneEvent:
			counts.DoneSeen = true
			counts.StopReason = e.StopReasonNormalized
			if e.Usage != nil {
				counts.UsageSeen = true
			}
		case anthropic.ErrorEvent:
			t.Fatalf("stream error: %v", e.Err)
		}
	}

	if !counts.DoneSeen {
		t.Fatalf("stream ended without DoneEvent")
	}

	return strings.TrimSpace(reply.String()), counts
}

type toolRunResult struct {
	Reply              string
	ToolSeen           bool
	DoneSeen           bool
	UsageSeen          bool
	StopSeen           bool
	AnswerSeen         bool
	ToolErrors         []string
	PostErrorToolCalls int
	PostErrorReply     string
}

func runToolStreaming(ctx context.Context, t *testing.T, client *anthropic.Client, tools []agentic.ToolDefinition, system, followup, userQuery string) toolRunResult {
	t.Helper()

	var (
		history   []message.AgentMessage
		args      struct{ A, B float64 }
		maxTurns  = 10
		out       strings.Builder
		result    toolRunResult
		debug     = os.Getenv("ANTHROPIC_E2E_DEBUG") != ""
		errorSent bool
	)

	for turn := 0; turn < maxTurns; turn++ {
		input := anthropic.Input{
			SystemPrompt: system,
			Tools:        tools,
			MaxTokens:    256,
		}
		if turn == 0 {
			input.UserMessage = userQuery
		} else {
			input.History = history
			input.UserMessage = followup
		}

		stream, err := client.Stream(ctx, input)
		if err != nil {
			t.Fatalf("tool stream %d failed: %v", turn+1, err)
		}

		reply, counts := collectStream(t, stream)
		if errorSent && counts.ToolCalls > 0 {
			result.PostErrorToolCalls += counts.ToolCalls
			result.PostErrorReply = reply
		}
		if debug {
			t.Logf("turn %d reply=%q tool_calls=%d stop_reason=%q usage=%v", turn+1, reply, counts.ToolCalls, counts.StopReason, counts.UsageSeen)
			for i, call := range counts.ToolCallList {
				t.Logf("turn %d tool_call[%d]=%s input=%s", turn+1, i, call.Name, string(call.Input))
			}
		}
		out.WriteString(reply)
		result.Reply = strings.TrimSpace(out.String())
		result.DoneSeen = result.DoneSeen || counts.DoneSeen
		result.UsageSeen = result.UsageSeen || counts.UsageSeen
		if counts.StopReason != "" {
			result.StopSeen = true
		}
		if counts.ToolCalls > 0 {
			result.ToolSeen = true
		}
		if strings.Contains(reply, "59") {
			result.AnswerSeen = true
			break
		}
		if len(reply) > 0 && counts.ToolCalls == 0 {
			break
		}
		if counts.ToolCalls == 0 {
			break
		}

		if turn == 0 && len(history) == 0 {
			history = append(history, message.AgentMessage{Role: message.RoleUser, Content: userQuery})
		}

		results := make([]agentic.ToolResult, 0, counts.ToolCalls)
		sentErrorThisTurn := false
		for _, call := range counts.ToolCallList {
			if call.Name != "add" {
				t.Fatalf("expected 'add' tool call, got: %s", call.Name)
			}
			var payload map[string]any
			if err := json.Unmarshal(call.Input, &payload); err != nil {
				errMsg := "invalid tool input: " + err.Error()
				result.ToolErrors = append(result.ToolErrors, errMsg)
				sentErrorThisTurn = true
				t.Logf("tool error returned: %s (raw=%s)", errMsg, string(call.Input))
				results = append(results, agentic.ToolResult{
					ID:   call.ID,
					Name: call.Name,
					Error: &agentic.ToolError{
						Message: errMsg,
					},
				})
				continue
			}
			if _, ok := payload["a"]; !ok {
				errMsg := "invalid tool input: missing field a"
				result.ToolErrors = append(result.ToolErrors, errMsg)
				sentErrorThisTurn = true
				t.Logf("tool error returned: %s (raw=%s)", errMsg, string(call.Input))
				results = append(results, agentic.ToolResult{
					ID:   call.ID,
					Name: call.Name,
					Error: &agentic.ToolError{
						Message: errMsg,
					},
				})
				continue
			}
			if _, ok := payload["b"]; !ok {
				errMsg := "invalid tool input: missing field b"
				result.ToolErrors = append(result.ToolErrors, errMsg)
				sentErrorThisTurn = true
				t.Logf("tool error returned: %s (raw=%s)", errMsg, string(call.Input))
				results = append(results, agentic.ToolResult{
					ID:   call.ID,
					Name: call.Name,
					Error: &agentic.ToolError{
						Message: errMsg,
					},
				})
				continue
			}
			_ = json.Unmarshal(call.Input, &args)
			resultPayload, _ := json.Marshal(map[string]float64{"sum": args.A + args.B})
			results = append(results, agentic.ToolResult{ID: call.ID, Name: call.Name, Output: resultPayload})
		}

		history = append(history,
			message.AgentMessage{Role: message.RoleAssistant, ToolCalls: counts.ToolCallList},
			message.AgentMessage{Role: message.RoleTool, ToolResults: results},
		)
		if sentErrorThisTurn {
			errorSent = true
		}
	}

	return result
}
