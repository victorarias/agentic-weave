package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/providers/anthropic"
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

func main() {
	if os.Getenv("ANTHROPIC_API_KEY") == "" || os.Getenv("ANTHROPIC_MODEL") == "" {
		fmt.Println("Set ANTHROPIC_API_KEY and ANTHROPIC_MODEL to run this example.")
		return
	}

	reg := agentic.NewRegistry()
	if err := reg.Register(AddTool{}); err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	client, err := anthropic.NewFromEnv()
	if err != nil {
		panic(err)
	}

	tools, err := reg.ListTools(ctx)
	if err != nil {
		panic(err)
	}

	fmt.Println("Streaming: add 10 and 32")
	first, err := streamOnce(ctx, client, anthropic.Input{
		SystemPrompt: "Use the add tool for math. After getting the result, state the answer.",
		UserMessage:  "add 10 and 32",
		Tools:        tools,
		MaxTokens:    256,
	})
	if err != nil {
		panic(err)
	}
	if len(first.Calls) == 0 {
		fmt.Println("No tool call returned. Reply:", strings.TrimSpace(first.Reply))
		return
	}

	var results []agentic.ToolResult
	for _, call := range first.Calls {
		call.Caller = &agentic.ToolCaller{Type: "llm"}
		result, err := reg.Execute(ctx, call)
		if err != nil {
			panic(err)
		}
		results = append(results, result)
	}

	history := []message.AgentMessage{
		{Role: message.RoleUser, Content: "add 10 and 32"},
		{Role: message.RoleAssistant, ToolCalls: first.Calls},
		{Role: message.RoleTool, ToolResults: results},
	}

	fmt.Println("\nStreaming: final answer")
	final, err := streamOnce(ctx, client, anthropic.Input{
		SystemPrompt: "Use the add tool for math. After getting the result, state the answer.",
		Tools:        tools,
		MaxTokens:    256,
		History:      history,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(strings.TrimSpace(final.Reply))
}

type streamResult struct {
	Reply string
	Calls []agentic.ToolCall
}

func streamOnce(ctx context.Context, client *anthropic.Client, input anthropic.Input) (streamResult, error) {
	stream, err := client.Stream(ctx, input)
	if err != nil {
		return streamResult{}, err
	}

	var reply strings.Builder
	calls := make([]agentic.ToolCall, 0)
	for event := range stream {
		switch e := event.(type) {
		case anthropic.TextDeltaEvent:
			fmt.Print(e.Delta)
			reply.WriteString(e.Delta)
		case anthropic.ToolCallEvent:
			calls = append(calls, e.Call)
		case anthropic.ErrorEvent:
			return streamResult{}, e.Err
		}
	}
	fmt.Println()
	return streamResult{Reply: reply.String(), Calls: calls}, nil
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
