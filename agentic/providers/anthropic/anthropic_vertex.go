package anthropic

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/vertex"
)

// VertexConfig controls an Anthropic client that routes through Vertex AI.
type VertexConfig struct {
	Project     string
	Location    string
	Model       string
	MaxTokens   int
	Temperature *float64
}

// NewVertex constructs an Anthropic client that uses Vertex AI as the backend.
// Authentication uses Google Application Default Credentials.
func NewVertex(ctx context.Context, cfg VertexConfig) (client *Client, err error) {
	project := cfg.Project
	location := cfg.Location
	model := cfg.Model
	if project == "" || model == "" {
		return nil, errors.New("anthropic vertex: project and model are required")
	}
	if location == "" {
		location = "us-east5"
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	// The SDK's vertex helpers panic on credential errors instead of returning them.
	defer func() {
		if r := recover(); r != nil {
			client = nil
			err = fmt.Errorf("anthropic vertex: %v", r)
		}
	}()

	vertexOpt := vertex.WithGoogleAuth(ctx, location, project)
	sdkClient := sdk.NewClient(vertexOpt)

	return &Client{
		client:      sdkClient,
		model:       model,
		maxTokens:   maxTokens,
		temperature: cfg.Temperature,
	}, nil
}

// NewFromVertexEnv builds an Anthropic-on-Vertex client from environment variables.
// Required: VERTEX_PROJECT, VERTEX_MODEL.
// Optional: VERTEX_LOCATION (default us-east5), VERTEX_MAX_TOKENS, VERTEX_TEMPERATURE.
func NewFromVertexEnv() (*Client, error) {
	project := envTrimmed("VERTEX_PROJECT")
	model := envTrimmed("VERTEX_MODEL")
	if project == "" || model == "" {
		return nil, errors.New("anthropic vertex: VERTEX_PROJECT and VERTEX_MODEL are required")
	}

	location := envTrimmed("VERTEX_LOCATION")

	maxTokens := 0
	if v := envTrimmed("VERTEX_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTokens = n
		}
	}

	var temperature *float64
	if v := envTrimmed("VERTEX_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			temperature = &f
		}
	}

	return NewVertex(context.Background(), VertexConfig{
		Project:     project,
		Location:    location,
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	})
}
