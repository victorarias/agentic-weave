package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/history"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/truncate"
)

type EchoTool struct{}

type EchoInput struct {
	Text string `json:"text"`
}

type EchoOutput struct {
	Text   string `json:"text"`
	Length int    `json:"length"`
}

func (EchoTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        "echo",
		Description: "Echoes input text back.",
	}
}

func (EchoTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	var input EchoInput
	if err := json.Unmarshal(call.Input, &input); err != nil {
		return agentic.ToolResult{Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	output := EchoOutput{Text: input.Text, Length: len(input.Text)}
	data, _ := json.Marshal(output)
	return agentic.ToolResult{Name: call.Name, Output: data}, nil
}

type SimpleDecider struct{}

func (SimpleDecider) Decide(ctx context.Context, in loop.Input) (loop.Decision, error) {
	if len(in.ToolResults) == 0 && strings.Contains(strings.ToLower(in.UserMessage), "echo") {
		payload, _ := json.Marshal(EchoInput{Text: in.UserMessage})
		return loop.Decision{
			ToolCalls: []agentic.ToolCall{{Name: "echo", Input: payload}},
		}, nil
	}
	if len(in.ToolResults) > 0 {
		return loop.Decision{Reply: fmt.Sprintf("Tool said: %s", string(in.ToolResults[0].Output))}, nil
	}
	return loop.Decision{Reply: "Say 'echo hello' to see a tool call."}, nil
}

type SimpleCompactor struct{}

func (SimpleCompactor) Compact(ctx context.Context, messages []budget.Message) (string, error) {
	return fmt.Sprintf("Summary: compacted %d messages.", len(messages)), nil
}

func main() {
	ctx := context.Background()
	reg := agentic.NewRegistry()
	if err := reg.Register(EchoTool{}); err != nil {
		log.Fatal(err)
	}

	store := history.NewMemoryStore()

	budgetMgr := &budget.Manager{
		Counter:   budget.CharCounter{},
		Compactor: SimpleCompactor{},
		Policy: budget.Policy{
			ContextWindow:    200,
			ReserveTokens:    20,
			KeepRecentTokens: 80,
		},
	}

	runner := loop.New(loop.Config{
		Decider:        SimpleDecider{},
		Executor:       reg,
		HistoryStore:   store,
		Budget:         budgetMgr,
		Truncation:     &truncate.Options{MaxLines: 10, MaxBytes: 1024},
		TruncationMode: truncate.ModeTail,
		Events: events.SinkFunc(func(e events.Event) {
			switch e.Type {
			case events.ToolOutputTruncated:
				fmt.Println("[event] tool output truncated:", e.Content)
			case events.ContextCompactionStart:
				fmt.Println("[event] compacting context...")
			case events.ContextCompactionEnd:
				fmt.Println("[event] compaction complete:", e.Content)
			}
		}),
	})

	result, err := runner.Run(ctx, loop.Request{
		SystemPrompt: "You are a helpful assistant.",
		UserMessage:  "echo hello from mono-like",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Reply:", result.Reply)
}
