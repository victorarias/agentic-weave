package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/victorarias/agentic-weave/adapters/gemini"
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

// Minimal Gemini-style function call model (simplified).
type geminiMessage struct {
	Role  string
	Parts []geminiPart
}

type geminiPart struct {
	Text     string
	Function *functionCall
	Result   *functionResult
}

type functionCall struct {
	Name string
	Args json.RawMessage
}

type functionResult struct {
	Name   string
	Result json.RawMessage
}

func main() {
	caps := gemini.Adapter{}.Capabilities()
	fmt.Printf("Gemini capabilities: tool_use=%v batching=%v vision=%v\n", caps.ToolUse, caps.Batching, caps.Vision)

	reg := agentic.NewRegistry()
	if err := reg.Register(AddTool{}); err != nil {
		panic(err)
	}

	ctx := context.Background()
	messages := []geminiMessage{{Role: "user", Parts: []geminiPart{{Text: "add 10 and 32"}}}}

	assistant := fakeGemini(messages)
	messages = append(messages, assistant)

	for _, part := range assistant.Parts {
		if part.Function == nil {
			continue
		}
		call := agentic.ToolCall{Name: part.Function.Name, Input: part.Function.Args, Caller: &agentic.ToolCaller{Type: "llm"}}
		result, err := reg.Execute(ctx, call)
		if err != nil {
			panic(err)
		}
		messages = append(messages, geminiMessage{Role: "tool", Parts: []geminiPart{{Result: &functionResult{Name: call.Name, Result: result.Output}}}})
	}

	final := fakeGemini(messages)
	for _, part := range final.Parts {
		if part.Text != "" {
			fmt.Println(strings.TrimSpace(part.Text))
		}
	}
}

// fakeGemini returns a function call on the first turn and a final response after tool result.
func fakeGemini(messages []geminiMessage) geminiMessage {
	last := messages[len(messages)-1]
	if last.Role == "user" {
		return geminiMessage{Role: "model", Parts: []geminiPart{{
			Function: &functionCall{Name: "add", Args: mustJSON(AddInput{A: 10, B: 32})},
		}}}
	}
	return geminiMessage{Role: "model", Parts: []geminiPart{{Text: "The sum is 42."}}}
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
