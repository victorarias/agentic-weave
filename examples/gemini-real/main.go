package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"google.golang.org/genai"
)

type AddParams struct {
	A int `json:"a"`
	B int `json:"b"`
}

type AddResult struct {
	Sum int `json:"sum"`
}

func main() {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		log.Fatal("GOOGLE_API_KEY is required")
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		log.Fatal(err)
	}

	model := client.Models.Get("gemini-2.0-flash")
	params := &genai.GenerateContentConfig{
		Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{
			{Name: "add", Description: "Add two integers", Parameters: addSchema()},
		}}},
	}

	resp, err := model.GenerateContent(ctx, genai.Text("Add 10 and 32"), params)
	if err != nil {
		log.Fatal(err)
	}

	for _, part := range resp.FunctionCalls() {
		if part.Name != "add" {
			continue
		}
		var args AddParams
		if err := json.Unmarshal(part.Args, &args); err != nil {
			log.Fatal(err)
		}
		result := AddResult{Sum: args.A + args.B}
		resultPart := genai.NewPartFromFunctionResponse(part.Name, result)

		followUp, err := model.GenerateContent(ctx, resultPart, nil)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(followUp.Text())
	}
}

func addSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"a": {Type: genai.TypeInteger},
			"b": {Type: genai.TypeInteger},
		},
		Required: []string{"a", "b"},
	}
}
