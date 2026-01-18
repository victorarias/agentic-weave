package toolscope

import (
	"context"
	"testing"
)

func TestScopeRoundTrip(t *testing.T) {
	scope := ToolScope{UserID: 42, ConversationID: "c1", Platform: "web"}
	ctx := WithScope(context.Background(), scope)
	got, ok := ScopeFromContext(ctx)
	if !ok {
		t.Fatalf("expected scope in context")
	}
	if got.UserID != scope.UserID || got.ConversationID != scope.ConversationID || got.Platform != scope.Platform {
		t.Fatalf("unexpected scope: %#v", got)
	}
}
