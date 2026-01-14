package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/invopop/jsonschema"
)

type AddParams struct {
	A int `json:"a"`
	B int `json:"b"`
}

type AddResult struct {
	Sum int `json:"sum"`
}

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is required")
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	tool := anthropic.ToolParam{
		Name:        "add",
		Description: anthropic.String("Add two integers."),
		InputSchema: jsonschema.Reflect(AddParams{}),
	}

	ctx := context.Background()
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model: anthropic.F(anthropic.ModelClaude3_5SonnetLatest),
		Messages: anthropic.F([]anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Add 10 and 32")),
		}),
		Tools: anthropic.F([]anthropic.ToolParam{tool}),
		MaxTokens: anthropic.F(int64(256)),
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, block := range msg.Content {
		if block.Type != "tool_use" {
			continue
		}
		var params AddParams
		if err := json.Unmarshal(block.Input, &params); err != nil {
			log.Fatal(err)
		}
		result := AddResult{Sum: params.A + params.B}
		toolResult := anthropic.NewToolResultBlock(block.ID, result, nil)

		followUp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model: anthropic.F(anthropic.ModelClaude3_5SonnetLatest),
			Messages: anthropic.F([]anthropic.MessageParam{
				anthropic.NewUserMessage(toolResult),
			}),
			MaxTokens: anthropic.F(int64(256)),
		})
		if err != nil {
			log.Fatal(err)
		}
		for _, followBlock := range followUp.Content {
			if followBlock.Type == "text" {
				fmt.Println(followBlock.Text)
			}
		}
	}
}
