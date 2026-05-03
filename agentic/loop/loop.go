package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/history"
	"github.com/victorarias/agentic-weave/agentic/message"
	"github.com/victorarias/agentic-weave/agentic/truncate"
	"github.com/victorarias/agentic-weave/agentic/usage"
)

// Decider chooses between replying or calling tools.
type Decider interface {
	Decide(ctx context.Context, in Input) (Decision, error)
}

// BeforeNextModelCallHook can append externally sourced messages immediately
// before the runner asks the model for another decision.
type BeforeNextModelCallHook interface {
	BeforeNextModelCall(ctx context.Context, in BeforeNextModelCallInput) ([]message.AgentMessage, error)
}

// BeforeNextModelCallHookFunc adapts a function to BeforeNextModelCallHook.
type BeforeNextModelCallHookFunc func(ctx context.Context, in BeforeNextModelCallInput) ([]message.AgentMessage, error)

func (f BeforeNextModelCallHookFunc) BeforeNextModelCall(ctx context.Context, in BeforeNextModelCallInput) ([]message.AgentMessage, error) {
	return f(ctx, in)
}

// Input captures state for a single decision step.
type Input struct {
	SystemPrompt   string
	UserMessage    string
	History        []message.AgentMessage
	Tools          []agentic.ToolDefinition
	ToolCalls      []agentic.ToolCall
	ToolResults    []agentic.ToolResult
	UserInlineData []agentic.InlineData // Images from the initial user message (first turn only).
	Turn           int
}

// BeforeNextModelCallInput captures the loop state visible to a pre-model hook.
type BeforeNextModelCallInput struct {
	SystemPrompt string
	UserMessage  string
	History      []message.AgentMessage
	Tools        []agentic.ToolDefinition
	ToolCalls    []agentic.ToolCall
	ToolResults  []agentic.ToolResult
	Turn         int
	CanContinue  bool
}

// Decision is the result of a decision step.
//
// Reasoning is the textual reasoning trace produced by the Decider on this
// step. It is empty for non-reasoning models. The runner records it on the
// resulting AgentMessage so the next turn can round-trip it (DeepSeek-style
// providers 400 if a prior assistant turn that produced reasoning is replayed
// without its reasoning_content).
type Decision struct {
	Reply      string
	Reasoning  string
	ToolCalls  []agentic.ToolCall
	Usage      *usage.Usage
	StopReason usage.StopReason
}

// Config controls the loop behavior.
type Config struct {
	Decider             Decider
	Executor            agentic.ToolExecutor
	HistoryStore        history.Store
	Budget              *budget.Manager
	Truncation          *truncate.Options
	TruncationMode      truncate.Mode
	Events              events.Sink
	BeforeNextModelCall BeforeNextModelCallHook
	MaxTurns            int
	ToolCallerType      string
}

// Request provides the conversation input.
type Request struct {
	SystemPrompt   string
	UserMessage    string
	History        []message.AgentMessage
	UserInlineData []agentic.InlineData // Images attached to the user message.
}

// Result captures the final output.
type Result struct {
	Reply       string
	History     []message.AgentMessage
	Summary     string
	ToolCalls   []agentic.ToolCall
	ToolResults []agentic.ToolResult
	Usage       *usage.Usage
	StopReason  usage.StopReason
	Exhausted   bool // true when the loop exited because it hit MaxTurns
	Steps       int  // number of LLM Decide() calls in this turn
}

// Runner executes a tool-aware loop with optional compaction and truncation.
type Runner struct {
	cfg Config
}

// New creates a new Runner.
func New(cfg Config) *Runner {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 3
	}
	if cfg.ToolCallerType == "" {
		cfg.ToolCallerType = "llm"
	}
	if cfg.TruncationMode == "" {
		cfg.TruncationMode = truncate.ModeTail
	}
	return &Runner{cfg: cfg}
}

// emit sends an event if a sink is configured.
func (r *Runner) emit(e events.Event) {
	if r.cfg.Events != nil {
		r.cfg.Events.Emit(e)
	}
}

// recordAssistantMessage stores an assistant message in history and emits MessageEnd.
func (r *Runner) recordAssistantMessage(ctx context.Context, turn int, reply, reasoning string, toolCalls []agentic.ToolCall, history *[]message.AgentMessage, isFinal bool) error {
	msg := message.AgentMessage{
		Role:             message.RoleAssistant,
		Content:          reply,
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
		Timestamp:        time.Now(),
	}
	*history = append(*history, msg)
	if err := r.appendHistory(ctx, msg); err != nil {
		return err
	}

	msgID := fmt.Sprintf("msg-%d", turn)
	if isFinal {
		msgID = fmt.Sprintf("msg-final-%d", turn)
	}
	r.emit(events.Event{
		Type:      events.MessageEnd,
		MessageID: msgID,
		Role:      message.RoleAssistant,
		Content:   reply,
		ToolCalls: toolCalls,
	})
	return nil
}

// Run executes the loop for a single user request.
func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	if r.cfg.Decider == nil {
		return Result{}, errors.New("loop: decider is required")
	}
	if err := r.validateConfig(); err != nil {
		return Result{}, err
	}

	r.emit(events.Event{Type: events.AgentStart})
	defer r.emit(events.Event{Type: events.AgentEnd})
	r.emit(events.Event{Type: events.TurnStart})
	defer r.emit(events.Event{Type: events.TurnEnd})

	historyMessages, err := r.loadHistory(ctx, req)
	if err != nil {
		return Result{}, err
	}

	userMessage := strings.TrimSpace(req.UserMessage)
	if userMessage != "" || len(req.UserInlineData) > 0 {
		userMsg := message.AgentMessage{
			Role:       message.RoleUser,
			Content:    userMessage,
			InlineData: req.UserInlineData,
			Timestamp:  time.Now(),
		}
		historyMessages = append(historyMessages, userMsg)
		if err := r.appendHistory(ctx, userMsg); err != nil {
			return Result{}, err
		}
	}

	summary, historyMessages, err := r.applyCompaction(ctx, historyMessages)
	if err != nil {
		return Result{}, err
	}

	tools, err := r.listTools(ctx)
	if err != nil {
		return Result{}, err
	}

	// Extract tool calls and results from history for the current turn
	toolCalls, toolResults := extractToolsFromHistory(historyMessages)

	var aggregatedUsage usage.Usage
	step := 0
	toolTurns := 0
	hookContinuations := 0
	runID := time.Now().UnixNano()
	for {
		hookMessages, err := r.beforeNextModelCall(ctx, BeforeNextModelCallInput{
			SystemPrompt: req.SystemPrompt,
			UserMessage:  userMessage,
			History:      historyMessages,
			Tools:        tools,
			ToolCalls:    toolCalls,
			ToolResults:  toolResults,
			Turn:         step,
			CanContinue:  true,
		})
		if err != nil {
			return Result{}, err
		}
		if len(hookMessages) > 0 {
			if err := r.appendMessages(ctx, hookMessages, &historyMessages); err != nil {
				return Result{}, err
			}
			toolCalls, toolResults = extractToolsFromHistory(historyMessages)
		}

		// Only pass user inline data on the first Decide() call — after
		// that it's already in history as part of the user message.
		var turnInlineData []agentic.InlineData
		if step == 0 {
			turnInlineData = req.UserInlineData
		}

		decision, err := r.cfg.Decider.Decide(ctx, Input{
			SystemPrompt:   req.SystemPrompt,
			UserMessage:    userMessage,
			History:        historyMessages,
			Tools:          tools,
			ToolCalls:      toolCalls,
			ToolResults:    toolResults,
			UserInlineData: turnInlineData,
			Turn:           step,
		})
		if err != nil {
			return Result{}, err
		}

		aggregatedUsage = usage.Add(aggregatedUsage, decision.Usage)

		for i := range decision.ToolCalls {
			if decision.ToolCalls[i].ID == "" {
				decision.ToolCalls[i].ID = fmt.Sprintf("call-%d-%d-%d", runID, step, i)
			}
			if decision.ToolCalls[i].Caller == nil {
				decision.ToolCalls[i].Caller = &agentic.ToolCaller{Type: r.cfg.ToolCallerType}
			}
		}

		if len(decision.ToolCalls) == 0 {
			canContinue := hookContinuations < r.cfg.MaxTurns
			pendingAssistant := message.AgentMessage{
				Role:             message.RoleAssistant,
				Content:          decision.Reply,
				ReasoningContent: decision.Reasoning,
				Timestamp:        time.Now(),
			}
			hookMessages, err := r.beforeNextModelCall(ctx, BeforeNextModelCallInput{
				SystemPrompt: req.SystemPrompt,
				UserMessage:  userMessage,
				History:      append(historyMessages, pendingAssistant),
				Tools:        tools,
				ToolCalls:    toolCalls,
				ToolResults:  toolResults,
				Turn:         step,
				CanContinue:  canContinue,
			})
			if err != nil {
				return Result{}, err
			}
			if len(hookMessages) > 0 {
				if !canContinue {
					if err := r.recordAssistantMessage(ctx, step, decision.Reply, decision.Reasoning, nil, &historyMessages, true); err != nil {
						return Result{}, err
					}
					return Result{
						Reply:       decision.Reply,
						History:     historyMessages,
						Summary:     summary,
						ToolCalls:   toolCalls,
						ToolResults: toolResults,
						Usage:       &aggregatedUsage,
						StopReason:  decision.StopReason,
						Exhausted:   true,
						Steps:       step + 1,
					}, nil
				}
				if err := r.recordAssistantMessage(ctx, step, decision.Reply, decision.Reasoning, nil, &historyMessages, false); err != nil {
					return Result{}, err
				}
				if err := r.appendMessages(ctx, hookMessages, &historyMessages); err != nil {
					return Result{}, err
				}
				toolCalls, toolResults = extractToolsFromHistory(historyMessages)
				hookContinuations++
				step++
				continue
			}

			if err := r.recordAssistantMessage(ctx, step, decision.Reply, decision.Reasoning, nil, &historyMessages, true); err != nil {
				return Result{}, err
			}

			return Result{
				Reply:       decision.Reply,
				History:     historyMessages,
				Summary:     summary,
				ToolCalls:   toolCalls,
				ToolResults: toolResults,
				Usage:       &aggregatedUsage,
				StopReason:  decision.StopReason,
				Exhausted:   false,
				Steps:       step + 1,
			}, nil
		}

		if toolTurns >= r.cfg.MaxTurns {
			if err := r.recordAssistantMessage(ctx, step, decision.Reply, decision.Reasoning, decision.ToolCalls, &historyMessages, true); err != nil {
				return Result{}, err
			}
			return Result{
				Reply:       decision.Reply,
				History:     historyMessages,
				Summary:     summary,
				ToolCalls:   append(toolCalls, decision.ToolCalls...),
				ToolResults: toolResults,
				Usage:       &aggregatedUsage,
				StopReason:  decision.StopReason,
				Exhausted:   true,
				Steps:       step + 1,
			}, nil
		}

		if r.cfg.Executor == nil {
			return Result{}, errors.New("loop: tool calls requested but no executor configured")
		}

		if err := r.recordAssistantMessage(ctx, step, decision.Reply, decision.Reasoning, decision.ToolCalls, &historyMessages, false); err != nil {
			return Result{}, err
		}

		for _, call := range decision.ToolCalls {
			r.emit(events.Event{Type: events.ToolStart, ToolCall: &call})

			result, err := r.cfg.Executor.Execute(ctx, call)
			if err != nil {
				result = agentic.ToolResult{
					ID:    call.ID,
					Name:  call.Name,
					Error: &agentic.ToolError{Message: err.Error()},
				}
			}
			if result.ID == "" {
				result.ID = call.ID
			}
			if result.Name == "" {
				result.Name = call.Name
			}

			if r.cfg.Truncation != nil {
				before := result
				trunc := truncate.Result{}
				result, trunc = truncateToolResult(result, r.cfg.TruncationMode, *r.cfg.Truncation)
				if trunc.Truncated {
					r.emit(events.Event{
						Type:       events.ToolOutputTruncated,
						ToolResult: &before,
						Content:    truncSummary(trunc),
					})
				}
			}

			r.emit(events.Event{Type: events.ToolEnd, ToolResult: &result})

			toolCalls = append(toolCalls, call)
			toolResults = append(toolResults, result)

			// Add tool result as structured message
			toolMsg := message.AgentMessage{
				Role:        message.RoleTool,
				ToolResults: []agentic.ToolResult{result},
				Timestamp:   time.Now(),
			}
			historyMessages = append(historyMessages, toolMsg)
			if err := r.appendHistory(ctx, toolMsg); err != nil {
				return Result{}, err
			}
		}
		toolTurns++
		step++
	}
}

func (r *Runner) validateConfig() error {
	if r.cfg.Budget == nil || r.cfg.HistoryStore == nil {
		return nil
	}
	if r.cfg.Budget.Counter == nil || r.cfg.Budget.Compactor == nil || r.cfg.Budget.Policy.ContextWindow <= 0 {
		return nil
	}
	if _, ok := r.cfg.HistoryStore.(history.Rewriter); !ok {
		return errors.New("loop: history store must implement history.Rewriter when budget compaction is enabled")
	}
	return nil
}

func (r *Runner) listTools(ctx context.Context) ([]agentic.ToolDefinition, error) {
	if r.cfg.Executor == nil {
		return nil, nil
	}
	return r.cfg.Executor.ListTools(ctx)
}

func (r *Runner) loadHistory(ctx context.Context, req Request) ([]message.AgentMessage, error) {
	if r.cfg.HistoryStore == nil {
		return append([]message.AgentMessage(nil), req.History...), nil
	}
	stored, err := r.cfg.HistoryStore.Load(ctx)
	if err != nil {
		return nil, err
	}
	if len(req.History) == 0 {
		return stored, nil
	}
	merged := make([]message.AgentMessage, 0, len(stored)+len(req.History))
	merged = append(merged, stored...)
	merged = append(merged, req.History...)
	return merged, nil
}

func (r *Runner) appendHistory(ctx context.Context, msg message.AgentMessage) error {
	if r.cfg.HistoryStore == nil {
		return nil
	}
	return r.cfg.HistoryStore.Append(ctx, msg)
}

func (r *Runner) appendMessages(ctx context.Context, messages []message.AgentMessage, history *[]message.AgentMessage) error {
	for _, msg := range messages {
		*history = append(*history, msg)
		if err := r.appendHistory(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) beforeNextModelCall(ctx context.Context, in BeforeNextModelCallInput) ([]message.AgentMessage, error) {
	if r.cfg.BeforeNextModelCall == nil {
		return nil, nil
	}
	in.History = cloneAgentMessages(in.History)
	in.Tools = cloneToolDefinitions(in.Tools)
	in.ToolCalls = cloneToolCalls(in.ToolCalls)
	in.ToolResults = cloneToolResults(in.ToolResults)
	return r.cfg.BeforeNextModelCall.BeforeNextModelCall(ctx, in)
}

func cloneAgentMessages(messages []message.AgentMessage) []message.AgentMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]message.AgentMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		out[i].ToolCalls = cloneToolCalls(msg.ToolCalls)
		out[i].ToolResults = cloneToolResults(msg.ToolResults)
		out[i].InlineData = cloneInlineData(msg.InlineData)
	}
	return out
}

func cloneToolDefinitions(defs []agentic.ToolDefinition) []agentic.ToolDefinition {
	if len(defs) == 0 {
		return nil
	}
	out := make([]agentic.ToolDefinition, len(defs))
	for i, def := range defs {
		out[i] = def
		out[i].InputSchema = cloneBytes(def.InputSchema)
		out[i].AllowedCallers = append([]string(nil), def.AllowedCallers...)
		if len(def.Examples) > 0 {
			out[i].Examples = make([]agentic.ToolExample, len(def.Examples))
			for j, ex := range def.Examples {
				out[i].Examples[j] = ex
				out[i].Examples[j].Input = cloneBytes(ex.Input)
				out[i].Examples[j].Output = cloneBytes(ex.Output)
			}
		}
	}
	return out
}

func cloneToolCalls(calls []agentic.ToolCall) []agentic.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]agentic.ToolCall, len(calls))
	for i, call := range calls {
		out[i] = call
		out[i].Input = cloneBytes(call.Input)
		if call.Caller != nil {
			caller := *call.Caller
			out[i].Caller = &caller
		}
	}
	return out
}

func cloneToolResults(results []agentic.ToolResult) []agentic.ToolResult {
	if len(results) == 0 {
		return nil
	}
	out := make([]agentic.ToolResult, len(results))
	for i, result := range results {
		out[i] = result
		out[i].Output = cloneBytes(result.Output)
		out[i].InlineData = cloneInlineData(result.InlineData)
		if result.Error != nil {
			toolErr := *result.Error
			out[i].Error = &toolErr
		}
	}
	return out
}

func cloneInlineData(data []agentic.InlineData) []agentic.InlineData {
	if len(data) == 0 {
		return nil
	}
	out := make([]agentic.InlineData, len(data))
	for i, item := range data {
		out[i] = item
		out[i].Data = cloneBytes(item.Data)
	}
	return out
}

func cloneBytes[T ~[]byte](data T) T {
	if len(data) == 0 {
		return nil
	}
	return T(append([]byte(nil), data...))
}

func (r *Runner) applyCompaction(ctx context.Context, messages []message.AgentMessage) (string, []message.AgentMessage, error) {
	if r.cfg.Budget == nil {
		return "", messages, nil
	}
	r.emit(events.Event{Type: events.ContextCompactionStart})

	compacted, summary, changed, err := message.CompactIfNeeded(ctx, *r.cfg.Budget, messages)
	if err != nil {
		r.emit(events.Event{Type: events.ContextCompactionEnd})
		return "", messages, err
	}
	if !changed {
		r.emit(events.Event{Type: events.ContextCompactionEnd})
		return "", messages, nil
	}

	r.emit(events.Event{Type: events.ContextCompactionEnd, Content: summary})

	if r.cfg.HistoryStore != nil {
		if rewriter, ok := r.cfg.HistoryStore.(history.Rewriter); ok {
			if err := rewriter.Replace(ctx, compacted); err != nil {
				return "", messages, err
			}
		}
	}

	return summary, compacted, nil
}

// extractToolsFromHistory extracts tool calls and results from history messages.
func extractToolsFromHistory(messages []message.AgentMessage) ([]agentic.ToolCall, []agentic.ToolResult) {
	var calls []agentic.ToolCall
	var results []agentic.ToolResult
	for _, msg := range messages {
		calls = append(calls, msg.ToolCalls...)
		results = append(results, msg.ToolResults...)
	}
	return calls, results
}

func truncateToolResult(result agentic.ToolResult, mode truncate.Mode, opts truncate.Options) (agentic.ToolResult, truncate.Result) {
	switch mode {
	case truncate.ModeHead:
		return truncate.HeadToolResult(result, opts)
	default:
		return truncate.TailToolResult(result, opts)
	}
}

func truncSummary(result truncate.Result) string {
	if !result.Truncated {
		return ""
	}
	return fmt.Sprintf("truncated by %s (%d/%d lines, %d/%d bytes)", result.TruncatedBy, result.OutputLines, result.TotalLines, result.OutputBytes, result.TotalBytes)
}
