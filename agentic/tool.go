package agentic

import (
	"context"
	"encoding/json"
)

// ToolDefinition describes a tool and how it can be called.
type ToolDefinition struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	InputSchema    json.RawMessage `json:"input_schema,omitempty"`
	SchemaHash     string          `json:"schema_hash,omitempty"`
	Examples       []ToolExample   `json:"examples,omitempty"`
	AllowedCallers []string        `json:"allowed_callers,omitempty"`
	DeferLoad      bool            `json:"defer_load,omitempty"`
}

// ToolExample provides a concrete input/output example for a tool.
type ToolExample struct {
	Description string          `json:"description,omitempty"`
	Input       json.RawMessage `json:"input"`
	Output      json.RawMessage `json:"output,omitempty"`
}

// ToolCaller identifies the source of a tool call.
type ToolCaller struct {
	Type   string `json:"type"`
	ToolID string `json:"tool_id,omitempty"`
}

// ToolCall is a request to invoke a tool.
type ToolCall struct {
	ID               string          `json:"id,omitempty"`
	Name             string          `json:"name"`
	Input            json.RawMessage `json:"input"`
	SchemaHash       string          `json:"schema_hash,omitempty"`
	Caller           *ToolCaller     `json:"caller,omitempty"`
	ThoughtSignature string          `json:"thought_signature,omitempty"`
}

// ToolResult is the tool execution output.
type ToolResult struct {
	ID     string          `json:"id,omitempty"`
	Name   string          `json:"name"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  *ToolError      `json:"error,omitempty"`
}

// ToolError is a normalized tool error payload.
type ToolError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// Tool executes a single tool call.
type Tool interface {
	Definition() ToolDefinition
	Execute(ctx context.Context, call ToolCall) (ToolResult, error)
}

// ToolExecutor lists and executes tools.
type ToolExecutor interface {
	ListTools(ctx context.Context) ([]ToolDefinition, error)
	Execute(ctx context.Context, call ToolCall) (ToolResult, error)
}

// ToolSearcher can provide query-based tool discovery.
type ToolSearcher interface {
	SearchTools(ctx context.Context, query string) ([]ToolDefinition, error)
}

// ToolFetcher can load a tool on-demand when DeferLoad is true.
type ToolFetcher interface {
	FetchTool(ctx context.Context, name string) (Tool, error)
}
