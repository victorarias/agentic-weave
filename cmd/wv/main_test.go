package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/cmd/wv/session"
	"github.com/victorarias/agentic-weave/cmd/wv/tui/components"
)

type appReplyDecider struct {
	reply string
}

func (d appReplyDecider) Decide(_ context.Context, _ loop.Input) (loop.Decision, error) {
	return loop.Decision{Reply: d.reply}, nil
}

type appToolDecider struct{}

func (d appToolDecider) Decide(_ context.Context, in loop.Input) (loop.Decision, error) {
	if len(in.ToolResults) == 0 {
		payload, _ := json.Marshal(map[string]string{"text": "ping"})
		return loop.Decision{ToolCalls: []agentic.ToolCall{{Name: "echo", Input: payload}}}, nil
	}
	return loop.Decision{Reply: "done after tool"}, nil
}

type appEchoTool struct{}

func (appEchoTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{Name: "echo", Description: "Echo input"}
}

func (appEchoTool) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{ID: call.ID, Name: call.Name, Output: call.Input}, nil
}

type appBlockingDecider struct{}

func (appBlockingDecider) Decide(ctx context.Context, _ loop.Input) (loop.Decision, error) {
	<-ctx.Done()
	return loop.Decision{}, ctx.Err()
}

type stubReloader struct {
	loaded    []string
	reloadErr error
}

type stubHistoryResetter struct {
	replaceErr error
	last       []message.AgentMessage
	called     bool
}

func (r *stubReloader) Reload() error {
	return r.reloadErr
}

func (r *stubReloader) Loaded() []string {
	out := make([]string, len(r.loaded))
	copy(out, r.loaded)
	return out
}

func (s *stubHistoryResetter) Replace(_ context.Context, messages []message.AgentMessage) error {
	s.called = true
	s.last = make([]message.AgentMessage, len(messages))
	copy(s.last, messages)
	return s.replaceErr
}

func TestAppSubmitAndReceiveAssistantMessage(t *testing.T) {
	s, err := session.New(session.Config{
		Decider: appReplyDecider{reply: "assistant reply"},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	app := newApp("test-model", s, nil, "", time.Second)
	app.submit("hello")
	waitForIdle(t, app, 2*time.Second)

	if len(app.conversation) < 2 {
		t.Fatalf("expected at least user+assistant messages, got %d", len(app.conversation))
	}
	if !containsLine(app.conversation, "**User:** hello") {
		t.Fatalf("expected user message in conversation: %#v", app.conversation)
	}
	if !containsLine(app.conversation, "**Assistant:** assistant reply") {
		t.Fatalf("expected assistant message in conversation: %#v", app.conversation)
	}
	if !strings.Contains(app.chat.Value, "assistant reply") {
		t.Fatalf("expected chat body to include assistant reply, got %q", app.chat.Value)
	}
}

func TestAppSanitizesAssistantTerminalEscapes(t *testing.T) {
	s, err := session.New(session.Config{
		Decider: appReplyDecider{reply: "safe\x1b[31mred\x1b[0m"},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	app := newApp("test-model", s, nil, "", time.Second)
	app.submit("hello")
	waitForIdle(t, app, 2*time.Second)
	if strings.Contains(app.chat.Value, "\x1b") {
		t.Fatalf("expected rendered chat to remove escape bytes, got %q", app.chat.Value)
	}
	if !strings.Contains(app.chat.Value, "safe[31mred[0m") {
		t.Fatalf("expected visible text to remain, got %q", app.chat.Value)
	}
}

func TestAppToolEventsAppearInConversation(t *testing.T) {
	reg := agentic.NewRegistry()
	if err := reg.Register(appEchoTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	s, err := session.New(session.Config{
		Decider:  appToolDecider{},
		Executor: reg,
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	app := newApp("test-model", s, nil, "", time.Second)
	app.submit("run tool")
	waitForIdle(t, app, 2*time.Second)

	entries := app.tools.Entries()
	if len(entries) == 0 {
		t.Fatal("expected at least one tool entry")
	}
	last := entries[len(entries)-1]
	if last.Name != "echo" {
		t.Fatalf("expected tool name echo, got %#v", last)
	}
	if last.State != components.ToolStateSuccess {
		t.Fatalf("expected success tool state, got %#v", last.State)
	}
	if !containsLine(app.conversation, "**Assistant:** done after tool") {
		t.Fatalf("expected assistant final line, got %#v", app.conversation)
	}
}

func TestAppSlashCommands(t *testing.T) {
	s, err := session.New(session.Config{
		Decider: appReplyDecider{reply: "assistant reply"},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	loader := &stubReloader{loaded: []string{"one.lua", "two.lua"}}
	app := newApp("test-model", s, loader, "", time.Second)
	app.appendConversation("User", "before")
	app.tools.AddPending("call-1", "bash")

	app.submit("/help")
	if !containsPrefix(app.conversation, "**System:** Commands: /help, /clear, /reload, /cancel") {
		t.Fatalf("expected help output, got %#v", app.conversation)
	}

	app.submit("/reload")
	if !containsPrefix(app.conversation, "**System:** Reloaded 2 extension(s).") {
		t.Fatalf("expected reload output, got %#v", app.conversation)
	}

	app.submit("/clear")
	if len(app.conversation) != 0 {
		t.Fatalf("expected conversation cleared, got %#v", app.conversation)
	}
	if len(app.tools.Entries()) != 0 {
		t.Fatalf("expected tool entries cleared, got %#v", app.tools.Entries())
	}
}

func TestAppSlashReloadError(t *testing.T) {
	s, err := session.New(session.Config{
		Decider: appReplyDecider{reply: "assistant reply"},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	loader := &stubReloader{reloadErr: errors.New("boom")}
	app := newApp("test-model", s, loader, "", time.Second)
	app.submit("/reload")
	if !containsPrefix(app.conversation, "**System:** Reload failed: boom") {
		t.Fatalf("expected reload error output, got %#v", app.conversation)
	}
}

func TestAppSlashReloadWithNoExtensions(t *testing.T) {
	s, err := session.New(session.Config{
		Decider: appReplyDecider{reply: "assistant reply"},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	loader := &stubReloader{}
	app := newApp("test-model", s, loader, "", time.Second)
	app.submit("/reload")
	if !containsPrefix(app.conversation, "**System:** Reloaded. No extensions found.") {
		t.Fatalf("expected no-extensions output, got %#v", app.conversation)
	}
}

func TestConversationIsBounded(t *testing.T) {
	s, err := session.New(session.Config{
		Decider: appReplyDecider{reply: "assistant reply"},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	app := newApp("test-model", s, nil, "", time.Second)
	for i := 0; i < maxConversationEntries+20; i++ {
		app.appendConversation("User", "msg")
	}
	if len(app.conversation) != maxConversationEntries {
		t.Fatalf("expected bounded conversation size, got %d", len(app.conversation))
	}
}

func TestHorizontalRuleRender(t *testing.T) {
	var hr horizontalRule
	lines := hr.Render(5)
	if len(lines) != 1 || lines[0] != "-----" {
		t.Fatalf("unexpected horizontal rule output %#v", lines)
	}
}

func TestAppCancelCommandCancelsActiveRun(t *testing.T) {
	s, err := session.New(session.Config{
		Decider: appBlockingDecider{},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	app := newApp("test-model", s, nil, "", 30*time.Second)
	app.submit("long running")
	time.Sleep(10 * time.Millisecond)
	app.submit("/cancel")
	waitForIdle(t, app, 2*time.Second)
	if !containsPrefix(app.conversation, "**System:** Cancellation requested.") {
		t.Fatalf("expected cancellation feedback, got %#v", app.conversation)
	}
}

func TestConversationFromHistory(t *testing.T) {
	input := []message.AgentMessage{
		{Role: message.RoleUser, Content: "hello"},
		{Role: message.RoleAssistant, Content: "hi"},
		{Role: message.RoleTool, Content: "ignored"},
		{Role: message.RoleSystem, Content: "note"},
	}
	lines := conversationFromHistory(input)
	if len(lines) != 3 {
		t.Fatalf("expected 3 visible lines, got %#v", lines)
	}
	if lines[0] != "**User:** hello" || lines[1] != "**Assistant:** hi" || lines[2] != "**System:** note" {
		t.Fatalf("unexpected converted lines %#v", lines)
	}
}

func TestAppClearResetsPersistedHistory(t *testing.T) {
	s, err := session.New(session.Config{
		Decider: appReplyDecider{reply: "assistant reply"},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	resetter := &stubHistoryResetter{}
	app := newAppWithHistory(
		"test-model",
		s,
		nil,
		"",
		time.Second,
		[]message.AgentMessage{{Role: message.RoleUser, Content: "old"}},
		resetter,
	)
	app.submit("/clear")
	if !resetter.called {
		t.Fatal("expected persisted history resetter to be called")
	}
	if len(resetter.last) != 0 {
		t.Fatalf("expected reset with empty message slice, got %#v", resetter.last)
	}
}

func TestNewAppWithHistoryLoadsConversation(t *testing.T) {
	s, err := session.New(session.Config{
		Decider: appReplyDecider{reply: "assistant reply"},
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	app := newAppWithHistory(
		"test-model",
		s,
		nil,
		"",
		time.Second,
		[]message.AgentMessage{
			{Role: message.RoleUser, Content: "first"},
			{Role: message.RoleAssistant, Content: "second"},
		},
		nil,
	)
	if !containsLine(app.conversation, "**User:** first") || !containsLine(app.conversation, "**Assistant:** second") {
		t.Fatalf("expected conversation to load from history, got %#v", app.conversation)
	}
}

func waitForIdle(t *testing.T, app *app, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		app.OnTick()
		if !app.busy && app.runCancel == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for app to become idle")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func containsPrefix(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
