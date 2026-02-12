package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/message"
	provider "github.com/victorarias/agentic-weave/agentic/providers/anthropic"
	"github.com/victorarias/agentic-weave/cmd/wv/config"
	"github.com/victorarias/agentic-weave/cmd/wv/extensions"
	"github.com/victorarias/agentic-weave/cmd/wv/persist"
	"github.com/victorarias/agentic-weave/cmd/wv/sanitize"
	"github.com/victorarias/agentic-weave/cmd/wv/session"
	"github.com/victorarias/agentic-weave/cmd/wv/tools"
	"github.com/victorarias/agentic-weave/cmd/wv/tui"
	"github.com/victorarias/agentic-weave/cmd/wv/tui/components"
)

const maxConversationEntries = 240

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "wv:", err)
		os.Exit(1)
	}
}

func run() error {
	opts, err := parseCLIArgs(os.Args[1:])
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client, err := provider.New(provider.Config{
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
	})
	if err != nil {
		return err
	}

	workDir, err := os.Getwd()
	if err != nil {
		return err
	}
	sessionStore, err := persist.NewStore(workDir, opts.SessionID)
	if err != nil {
		return err
	}
	if opts.NewSession {
		if err := sessionStore.Replace(context.Background(), nil); err != nil {
			return err
		}
	}
	initialHistory, err := sessionStore.Load(context.Background())
	if err != nil {
		return err
	}

	reg := agentic.NewRegistry()
	if err := tools.RegisterBuiltins(reg, tools.Options{
		WorkDir:    workDir,
		EnableBash: cfg.EnableBash,
	}); err != nil {
		return err
	}
	var loader *extensions.Loader
	extensionNotice := ""
	if cfg.EnableExtensions {
		if !cfg.EnableProjectExtensions {
			extensionNotice = "Project extensions disabled (set WV_ENABLE_PROJECT_EXTENSIONS=1 to enable .wv/extensions)."
		}
		loader = extensions.NewLoaderWithOptions(workDir, extensions.Options{
			IncludeGlobal:  true,
			IncludeProject: cfg.EnableProjectExtensions,
		})
		defer loader.Close()
		if err := loader.Load(); err != nil {
			extensionNotice = "Extension load error: " + err.Error()
		}
	}

	sess, err := session.New(session.Config{
		Decider: anthropicDecider{
			client:      client,
			maxTokens:   cfg.MaxTokens,
			temperature: cfg.Temperature,
		},
		Executor:     reg,
		HistoryStore: sessionStore,
		SystemPrompt: cfg.SystemPrompt,
		MaxTurns:     cfg.MaxTurns,
	})
	if err != nil {
		return err
	}

	if opts.NonInteractive {
		messageValue, err := resolveNonInteractiveMessage(opts.Message, os.Stdin, stdinIsTTY())
		if err != nil {
			return err
		}
		return runNonInteractive(context.Background(), sess, messageValue, time.Duration(cfg.RunTimeoutSeconds)*time.Second, os.Stdout)
	}

	app := newAppWithHistory(
		cfg.Model,
		sess,
		loader,
		extensionNotice,
		time.Duration(cfg.RunTimeoutSeconds)*time.Second,
		initialHistory,
		sessionStore,
	)
	term := tui.NewTerminal(os.Stdin, os.Stdout)
	ui := tui.New(term, app.root, app.root)
	ui.SetOnTick(app.OnTick)

	err = ui.Run(context.Background(), 33*time.Millisecond)
	app.cancelActiveRun()
	if errors.Is(err, tui.ErrInterrupted) {
		return nil
	}
	return err
}

type app struct {
	session *session.Session

	header *components.Text
	chat   *components.Markdown
	tools  *components.ToolOutput
	status *components.Text
	editor *components.Editor
	loader *components.Loader
	root   *components.Container

	busy            bool
	conversation    []string
	streamingBuffer string
	streamingActive bool
	extensions      extensionReloader
	runTimeout      time.Duration
	runCancel       context.CancelFunc
	historyResetter historyResetter
}

type extensionReloader interface {
	Reload() error
	Loaded() []string
}

type historyResetter interface {
	Replace(ctx context.Context, messages []message.AgentMessage) error
}

func newApp(model string, sess *session.Session, ext extensionReloader, startupNotice string, runTimeout time.Duration) *app {
	return newAppWithHistory(model, sess, ext, startupNotice, runTimeout, nil, nil)
}

func newAppWithHistory(
	model string,
	sess *session.Session,
	ext extensionReloader,
	startupNotice string,
	runTimeout time.Duration,
	initialHistory []message.AgentMessage,
	resetter historyResetter,
) *app {
	if runTimeout <= 0 {
		runTimeout = 180 * time.Second
	}
	a := &app{
		session:         sess,
		header:          components.NewText("wv | provider: anthropic | model: " + model),
		chat:            components.NewMarkdown("Welcome to wv. Type a message and press Enter."),
		tools:           components.NewToolOutput(),
		status:          components.NewText("ready"),
		editor:          components.NewEditor("> "),
		loader:          components.NewLoader("thinking"),
		conversation:    make([]string, 0, 64),
		streamingBuffer: "",
		extensions:      ext,
		runTimeout:      runTimeout,
		historyResetter: resetter,
	}
	a.editor.SetSubmitHandler(a.submit)

	a.root = components.NewContainer(
		a.header,
		horizontalRule{},
		a.chat,
		horizontalRule{},
		a.tools,
		horizontalRule{},
		a.status,
		horizontalRule{},
		a.editor,
	)
	if len(initialHistory) > 0 {
		a.conversation = conversationFromHistory(initialHistory)
	}
	if strings.TrimSpace(startupNotice) != "" {
		a.appendConversation("System", startupNotice)
	} else {
		a.refreshChat()
	}
	return a
}

func (a *app) submit(msg string) {
	text := strings.TrimSpace(msg)
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		a.handleCommand(text)
		return
	}
	if a.busy {
		a.status.Set("agent is still running")
		return
	}

	a.appendConversation("User", text)
	runCtx, cancel := context.WithTimeout(context.Background(), a.runTimeout)
	a.runCancel = cancel
	if err := a.session.Send(runCtx, text); err != nil {
		cancel()
		a.runCancel = nil
		a.status.Set("error: " + err.Error())
	}
}

func (a *app) handleCommand(raw string) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "/help":
		a.appendConversation("System", "Commands: /help, /clear, /reload, /cancel")
	case "/clear":
		a.conversation = nil
		a.streamingActive = false
		a.streamingBuffer = ""
		a.tools.Clear()
		if a.historyResetter != nil {
			if err := a.historyResetter.Replace(context.Background(), nil); err != nil {
				a.appendConversation("System", "Failed to clear persisted session: "+err.Error())
			}
		}
		a.refreshChat()
		a.status.Set("ready")
	case "/cancel":
		if a.runCancel == nil {
			a.appendConversation("System", "No active run.")
			return
		}
		a.cancelActiveRun()
		a.status.Set("cancelling...")
		a.appendConversation("System", "Cancellation requested.")
	case "/reload":
		if a.extensions == nil {
			a.appendConversation("System", "No extension loader configured.")
			return
		}
		if err := a.extensions.Reload(); err != nil {
			a.appendConversation("System", "Reload failed: "+err.Error())
			return
		}
		count := len(a.extensions.Loaded())
		if count == 0 {
			a.appendConversation("System", "Reloaded. No extensions found.")
			return
		}
		a.appendConversation("System", fmt.Sprintf("Reloaded %d extension(s).", count))
	default:
		a.appendConversation("System", "Unknown command: "+fields[0])
	}
}

// OnTick drains session updates and refreshes status animation.
func (a *app) OnTick() {
	for {
		select {
		case update := <-a.session.Updates():
			a.applyUpdate(update)
		default:
			if a.busy {
				a.status.Set(a.loader.Render(80)[0])
			}
			return
		}
	}
}

func (a *app) applyUpdate(update session.Update) {
	switch update.Type {
	case session.UpdateRunStart:
		a.busy = true
		a.status.Set("- thinking")
	case session.UpdateRunEnd:
		a.busy = false
		a.runCancel = nil
		a.status.Set("ready")
	case session.UpdateRunError:
		a.busy = false
		a.runCancel = nil
		a.status.Set("error: " + update.Err.Error())
		a.appendConversation("System", "Error: "+update.Err.Error())
	case session.UpdateEvent:
		a.applyEvent(update.Event)
	}
}

func (a *app) cancelActiveRun() {
	if a.runCancel == nil {
		return
	}
	a.runCancel()
	a.runCancel = nil
}

func (a *app) applyEvent(e events.Event) {
	switch e.Type {
	case events.MessageStart:
		if e.Role == "assistant" {
			a.streamingActive = true
			a.streamingBuffer = ""
		}
	case events.MessageUpdate:
		if e.Role == "assistant" {
			a.streamingActive = true
			a.streamingBuffer += sanitize.Text(e.Delta)
			a.refreshChat()
		}
	case events.MessageEnd:
		if e.Role == "assistant" {
			text := strings.TrimSpace(sanitize.Text(e.Content))
			if text != "" {
				a.appendConversation("Assistant", text)
			}
			a.streamingActive = false
			a.streamingBuffer = ""
			a.refreshChat()
		}
	case events.ToolStart:
		if e.ToolCall != nil {
			a.tools.AddPending(e.ToolCall.ID, e.ToolCall.Name)
		}
	case events.ToolEnd:
		if e.ToolResult != nil {
			if e.ToolResult.Error != nil {
				summary, details, isError := tools.SummarizeToolUpdate(e.ToolResult)
				if summary == "" {
					summary = sanitize.Text(e.ToolResult.Error.Message)
				}
				a.tools.Resolve(e.ToolResult.ID, e.ToolResult.Name, summary, details, isError)
			} else {
				summary, details, _ := tools.SummarizeToolUpdate(e.ToolResult)
				if summary == "" {
					summary = "completed"
				}
				a.tools.Resolve(e.ToolResult.ID, e.ToolResult.Name, summary, details, false)
			}
		}
	}
}

func (a *app) appendConversation(role, content string) {
	content = strings.TrimSpace(content)
	content = sanitize.Text(content)
	if content == "" {
		return
	}
	a.conversation = append(a.conversation, "**"+role+":** "+content)
	if len(a.conversation) > maxConversationEntries {
		a.conversation = append([]string(nil), a.conversation[len(a.conversation)-maxConversationEntries:]...)
	}
	a.refreshChat()
}

func (a *app) refreshChat() {
	body := strings.Join(a.conversation, "\n\n")
	if a.streamingActive && strings.TrimSpace(a.streamingBuffer) != "" {
		if body != "" {
			body += "\n\n"
		}
		body += "**Assistant:** " + a.streamingBuffer
	}
	if strings.TrimSpace(body) == "" {
		body = "Welcome to wv. Type a message and press Enter."
	}
	a.chat.Set(body)
}

func conversationFromHistory(messages []message.AgentMessage) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(sanitize.Text(msg.Content))
		if content == "" {
			continue
		}
		switch msg.Role {
		case message.RoleUser:
			out = append(out, "**User:** "+content)
		case message.RoleAssistant:
			out = append(out, "**Assistant:** "+content)
		case message.RoleSystem:
			out = append(out, "**System:** "+content)
		}
	}
	return out
}

type horizontalRule struct{}

func (horizontalRule) Render(width int) []string {
	if width <= 0 {
		width = 80
	}
	return []string{strings.Repeat("-", width)}
}
