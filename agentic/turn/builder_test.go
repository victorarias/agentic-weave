package turn

import (
	"strings"
	"testing"

	"github.com/victorarias/agentic-weave/agentic/message"
)

func TestBuilderAssemble_NoSectionsReturnsUserMessageUnchanged(t *testing.T) {
	b := NewBuilder("system")
	assembled := b.Assemble("hello")

	assertEqual(t, "system", assembled.SystemPrompt)
	assertEqual(t, "hello", assembled.UserMessage)
	if len(assembled.SectionKeys) != 0 {
		t.Errorf("expected no section keys, got %v", assembled.SectionKeys)
	}
}

func TestBuilderSection_FormatsAndPreservesOrderIncludingDuplicateKeys(t *testing.T) {
	b := NewBuilder("system")
	b.Section(" memory_context ", "line-1")
	b.Section("memory_context", "line-2")

	assembled := b.Assemble("hello")

	assertSliceEqual(t, []string{"memory_context", "memory_context"}, assembled.SectionKeys)
	assertContains(t, assembled.UserMessage, "[runtime_context]\nSystem-generated context for this turn (not user-authored input).")
	assertContains(t, assembled.UserMessage, "[memory_context]\nline-1")
	assertContains(t, assembled.UserMessage, "[memory_context]\nline-2")

	first := strings.Index(assembled.UserMessage, "[memory_context]\nline-1")
	second := strings.Index(assembled.UserMessage, "[memory_context]\nline-2")
	if first == -1 || second == -1 || first >= second {
		t.Errorf("expected line-1 before line-2 in output")
	}
}

func TestBuilderSkipsEmptyKeyAndEmptyContent(t *testing.T) {
	b := NewBuilder("system")
	b.Section("", "value")
	b.Section("memory", "   ")
	b.Reminder("", "note")
	b.RawSection("raw", "\n\t\n")

	assembled := b.Assemble("hello")
	assertEqual(t, "hello", assembled.UserMessage)
	if len(assembled.SectionKeys) != 0 {
		t.Errorf("expected no section keys, got %v", assembled.SectionKeys)
	}
}

func TestBuilderReminder_WrapsInSystemReminderTag(t *testing.T) {
	b := NewBuilder("system")
	b.Reminder("soul", "SOUL UPDATED")

	assembled := b.Assemble("")

	expected := "<system-reminder>\nSOUL UPDATED\n</system-reminder>"
	assertContains(t, assembled.UserMessage, expected)
	assertSliceEqual(t, []string{"soul"}, assembled.SectionKeys)
}

func TestBuilderRawSection_PassesThroughVerbatim(t *testing.T) {
	b := NewBuilder("system")
	raw := "<already-formatted>\nline\n</already-formatted>"
	b.RawSection("x", raw)

	assembled := b.Assemble("user")

	assertContains(t, assembled.UserMessage, raw)
	assertSliceEqual(t, []string{"x"}, assembled.SectionKeys)
}

func TestBuilderAssemble_EmptyUserMessageWithSectionsStartsAtRuntimeContext(t *testing.T) {
	b := NewBuilder("system")
	b.Section("memory_context", "value")

	assembled := b.Assemble("")
	assertEqual(t, "[runtime_context]\nSystem-generated context for this turn (not user-authored input).\n\n[memory_context]\nvalue", assembled.UserMessage)
}

func TestBuilderSection_PreservesNonEmptyContentVerbatim(t *testing.T) {
	b := NewBuilder("system")
	b.Section("state", "  keep spacing  ")

	assembled := b.Assemble("user")
	assertContains(t, assembled.UserMessage, "[state]\n  keep spacing  ")
}

func TestBuilderRequest_PassthroughsHistoryAndAssembledMessage(t *testing.T) {
	history := []message.AgentMessage{{Role: message.RoleUser, Content: "before"}}

	b := NewBuilder("system")
	b.Section("memory_context", "value")

	req := b.Request("hello", history)

	assertEqual(t, "system", req.SystemPrompt)
	if len(req.History) != 1 || req.History[0].Content != "before" {
		t.Errorf("expected history passthrough, got %v", req.History)
	}
	assertContains(t, req.UserMessage, "hello\n\n[runtime_context]")
}

// --- helpers ---

func assertEqual(t *testing.T, expected, actual string) {
	t.Helper()
	if expected != actual {
		t.Errorf("expected %q, got %q", expected, actual)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected %q to contain %q", haystack, needle)
	}
}

func assertSliceEqual(t *testing.T, expected, actual []string) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Errorf("expected %v, got %v", expected, actual)
		return
	}
	for i := range expected {
		if expected[i] != actual[i] {
			t.Errorf("expected[%d] = %q, got %q", i, expected[i], actual[i])
		}
	}
}
