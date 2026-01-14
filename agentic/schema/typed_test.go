package schema

import (
	"encoding/json"
	"testing"
)

type sample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Note  string `json:"note,omitempty"`
}

func TestSchemaFromStruct(t *testing.T) {
	payload, err := SchemaFromStruct(sample{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}
	if decoded["type"] != "object" {
		t.Fatalf("expected object schema")
	}
	props := decoded["properties"].(map[string]any)
	if _, ok := props["name"]; !ok {
		t.Fatalf("missing name property")
	}
}
