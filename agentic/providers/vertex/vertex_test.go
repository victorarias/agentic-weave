package vertex

import (
	"encoding/json"
	"testing"
)

func TestVertexPartUnmarshalThoughtSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "snake_case",
			input:    `{"functionCall":{"name":"test","args":{}},"thought_signature":"sig123"}`,
			expected: "sig123",
		},
		{
			name:     "camelCase",
			input:    `{"functionCall":{"name":"test","args":{}},"thoughtSignature":"sig456"}`,
			expected: "sig456",
		},
		{
			name:     "snake_case takes precedence",
			input:    `{"functionCall":{"name":"test","args":{}},"thought_signature":"snake","thoughtSignature":"camel"}`,
			expected: "snake",
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
