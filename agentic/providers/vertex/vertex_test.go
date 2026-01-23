package vertex

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVertexPartMarshalThoughtSignature(t *testing.T) {
	part := vertexPart{
		FunctionCall: &vertexFunctionCall{
			Name: "test",
			Args: map[string]any{},
		},
		ThoughtSignature: "sig123",
	}

	data, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify output uses camelCase (thoughtSignature), not snake_case
	output := string(data)
	if !strings.Contains(output, `"thoughtSignature":"sig123"`) {
		t.Errorf("expected camelCase thoughtSignature in output, got: %s", output)
	}
	if strings.Contains(output, "thought_signature") {
		t.Errorf("unexpected snake_case thought_signature in output: %s", output)
	}
}

func TestVertexPartUnmarshalThoughtSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "camelCase (primary)",
			input:    `{"functionCall":{"name":"test","args":{}},"thoughtSignature":"sig456"}`,
			expected: "sig456",
		},
		{
			name:     "snake_case (fallback)",
			input:    `{"functionCall":{"name":"test","args":{}},"thought_signature":"sig123"}`,
			expected: "sig123",
		},
		{
			name:     "camelCase takes precedence over snake_case",
			input:    `{"functionCall":{"name":"test","args":{}},"thought_signature":"snake","thoughtSignature":"camel"}`,
			expected: "camel",
		},
		{
			name:     "no signature",
			input:    `{"functionCall":{"name":"test","args":{}}}`,
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var part vertexPart
			if err := json.Unmarshal([]byte(tc.input), &part); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if part.ThoughtSignature != tc.expected {
				t.Errorf("expected ThoughtSignature=%q, got %q", tc.expected, part.ThoughtSignature)
			}
		})
	}
}
