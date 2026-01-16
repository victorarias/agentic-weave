package harness

import (
	"testing"

	"github.com/victorarias/agentic-weave/agentic/providers/vertex"
	"golang.org/x/oauth2"
)

func TestVertexNewFromEnvRequiresProjectAndModel(t *testing.T) {
	t.Setenv("VERTEX_PROJECT", "")
	t.Setenv("VERTEX_MODEL", "")
	t.Setenv("VERTEX_LOCATION", "")
	t.Setenv("VERTEX_API_BASE", "")

	_, err := vertex.NewFromEnv()
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestVertexNewWithTokenSource(t *testing.T) {
	client, err := vertex.New(vertex.Config{
		Project:     "proj",
		Location:    "global",
		Model:       "model",
		TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "x"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatalf("expected client")
	}
}
