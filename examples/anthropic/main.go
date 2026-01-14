package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/victorarias/agentic-weave/adapters/anthropic"
	"github.com/victorarias/agentic-weave/agentic"
)

type AddTool struct{}

type AddInput struct {
	A int `json:"a"`
	B int `json:"b"`
}

type AddOutput struct {
	Sum int `json:"sum"`
}

func (AddTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        "add",
		Description: "Adds two integers.",
		Examples: []agentic.ToolExample{
			{Input: mustJSON(AddInput{A: 2, B: 3}), Output: mustJSON(AddOutput{Sum: 5})},
		},
		AllowedCallers: []string{"llm"},
	}
}

func (AddTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	var input AddInput
	if err := json.Unmarshal(call.Input, &input); err != nil {
		return agentic.ToolResult{Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	output := AddOutput{Sum: input.A + input.B}
	return agentic.ToolResult{Name: call.Name, Output: mustJSON(output)}, nil
}

// Minimal Anthropic message model (simplified).
type message struct {
	Role    string
	Content []contentBlock
}

type contentBlock struct {
	Type      string
	Text      string
	ToolName  string
	ToolID    string
	Input     json.RawMessage
	ToolResult json.RawMessage
}

func main() {
	caps := anthropic.Adapter{}.Capabilities()
	fmt.Printf("Anthropic capabilities: tool_use=%v tool_examples=%v tool_search=%v\n", caps.ToolUse, caps.ToolExamples, caps.ToolSearch)

	reg := agentic.NewRegistry()
	if err := reg.Register(AddTool{}); err != nil {
		panic(err)
	}

	ctx := context.Background()
	messages := []message{{Role: "user", Content: []contentBlock{{Type: "text", Text: "add 10 and 32"}}}}

	assistant := fakeClaude(messages)
	messages = append(messages, assistant)

	// Execute tool calls from assistant blocks.
	for _, block := range assistant.Content {
		if block.Type != "tool_use" {
			continue
		}
		call := agentic.ToolCall{
			ID:     block.ToolID,
			Name:   block.ToolName,
			Input:  block.Input,
			Caller: &agentic.ToolCaller{Type: "llm"},
		}
		result, err := reg.Execute(ctx, call)
		if err != nil {
			panic(err)
		}
		messages = append(messages, message{
			Role: "tool",
			Content: []contentBlock{{
				Type:       "tool_result",
				ToolName:   call.Name,
				ToolID:     call.ID,
				ToolResult: result.Output,
			}},
		})
	}

	final := fakeClaude(messages)
	for _, block := range final.Content {
		if block.Type == "text" {
			fmt.Println(strings.TrimSpace(block.Text))
		}
	}
}

// fakeClaude returns a tool_use block on the first turn and a final response after tool_result.
func fakeClaude(messages []message) message {
	last := messages[len(messages)-1]
	if last.Role == "user" {
		return message{Role: "assistant", Content: []contentBlock{{
			Type:     "tool_use",
			ToolName: "add",
			ToolID:   "tool-call-1",
			Input:    mustJSON(AddInput{A: 10, B: 32}),
		}}}
	}
	return message{Role: "assistant", Content: []contentBlock{{Type: "text", Text: "The sum is 42."}}}
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
