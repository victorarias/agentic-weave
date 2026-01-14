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
	A int `json:"a"` // jsonschema_description can be added
	B int `json:"b"`
}

type AddResult struct {
	Sum int `json:"sum"`
}

var addSchema = generateSchema[AddParams]()

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is required")
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	toolParams := []anthropic.ToolParam{
		{
			Name:        "add",
			Description: anthropic.String("Add two integers."),
			InputSchema: addSchema,
		},
	}
	tools := make([]anthropic.ToolUnionParam, len(toolParams))
	for i, toolParam := range toolParams {
		tools[i] = anthropic.ToolUnionParam{OfTool: &toolParam}
	}

	ctx := context.Background()
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("Add 10 and 32")),
	}

	for {
		msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeSonnet4_5_20250929,
			MaxTokens: 256,
			Messages:  messages,
			Tools:     tools,
		})
		if err != nil {
			log.Fatal(err)
		}

		messages = append(messages, msg.ToParam())

		toolResults := []anthropic.ContentBlockParamUnion{}
		for _, block := range msg.Content {
			switch variant := block.AsAny().(type) {
			case anthropic.TextBlock:
				fmt.Println(variant.Text)
			case anthropic.ToolUseBlock:
				var args AddParams
				if err := json.Unmarshal([]byte(variant.JSON.Input.Raw()), &args); err != nil {
					log.Fatal(err)
				}
				result := AddResult{Sum: args.A + args.B}
				b, err := json.Marshal(result)
				if err != nil {
					log.Fatal(err)
				}
				toolResults = append(toolResults, anthropic.NewToolResultBlock(variant.ID, string(b), false))
			}
		}

		if len(toolResults) == 0 {
			break
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}
}

func generateSchema[T any]() anthropic.ToolInputSchemaParam {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return anthropic.ToolInputSchemaParam{
		Properties: schema.Properties,
		Required:   schema.Required,
	}
}
