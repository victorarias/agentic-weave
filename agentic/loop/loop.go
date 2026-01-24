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

// Input captures state for a single decision step.
type Input struct {
	SystemPrompt string
	UserMessage  string
	History      []message.AgentMessage
	Tools        []agentic.ToolDefinition
	ToolCalls    []agentic.ToolCall
	ToolResults  []agentic.ToolResult
	Turn         int
}

// Decision is the result of a decision step.
type Decision struct {
	Reply      string
	ToolCalls  []agentic.ToolCall
	Usage      *usage.Usage
	StopReason usage.StopReason
}

// Config controls the loop behavior.
type Config struct {
	Decider        Decider
	Executor       agentic.ToolExecutor
	HistoryStore   history.Store
	Budget         *budget.Manager
	Truncation     *truncate.Options
	TruncationMode truncate.Mode
	Events         events.Sink
	MaxTurns       int
	ToolCallerType string
}

// Request provides the conversation input.
type Request struct {
	SystemPrompt string
	UserMessage  string
	History      []message.AgentMessage
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
	if userMessage != "" {
		userMsg := message.AgentMessage{
			Role:      message.RoleUser,
			Content:   userMessage,
			Timestamp: time.Now(),
		}
		historyMessages = append(historyMessages, userMsg)
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

	turn := 0
	for {
		decision, err := r.cfg.Decider.Decide(ctx, Input{
			SystemPrompt: req.SystemPrompt,
			UserMessage:  userMessage,
			History:      historyMessages,
			Tools:        tools,
			ToolCalls:    toolCalls,
			ToolResults:  toolResults,
			Turn:         turn,
		})
		if err != nil {
			return Result{}, err
		}

		if len(decision.ToolCalls) == 0 || turn >= r.cfg.MaxTurns {
			reply := strings.TrimSpace(decision.Reply)
			if reply == "" {
				reply = "I am here. Tell me what you need."
			}

			assistantMsg := message.AgentMessage{
				Role:      message.RoleAssistant,
				Content:   reply,
				Timestamp: time.Now(),
			}
			historyMessages = append(historyMessages, assistantMsg)
			r.appendHistory(ctx, assistantMsg)

			r.emit(events.Event{
				Type:      events.MessageEnd,
				MessageID: fmt.Sprintf("msg-final-%d", turn),
				Role:      message.RoleAssistant,
				Content:   reply,
			})

			return Result{
				Reply:       reply,
				History:     historyMessages,
				Summary:     summary,
				ToolCalls:   toolCalls,
				ToolResults: toolResults,
				Usage:       decision.Usage,
				StopReason:  decision.StopReason,
			}, nil
		}

		if r.cfg.Executor == nil {
			return Result{}, errors.New("loop: tool calls requested but no executor configured")
		}

		// Emit MessageEnd for assistant message with tool calls
		r.emit(events.Event{
			Type:      events.MessageEnd,
			MessageID: fmt.Sprintf("msg-%d", turn),
			Role:      message.RoleAssistant,
			Content:   decision.Reply,
			ToolCalls: decision.ToolCalls,
		})

		// Add assistant message with tool calls to history
		assistantMsg := message.AgentMessage{
			Role:      message.RoleAssistant,
			Content:   decision.Reply,
			ToolCalls: decision.ToolCalls,
			Timestamp: time.Now(),
		}
		historyMessages = append(historyMessages, assistantMsg)
		r.appendHistory(ctx, assistantMsg)

		for i, call := range decision.ToolCalls {
			if call.ID == "" {
				call.ID = fmt.Sprintf("call-%d-%d", turn, i)
			}
			if call.Caller == nil {
				call.Caller = &agentic.ToolCaller{Type: r.cfg.ToolCallerType}
			}

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
			r.appendHistory(ctx, toolMsg)
		}
		turn++
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
	return r.cfg.HistoryStore.Load(ctx)
}

func (r *Runner) appendHistory(ctx context.Context, msg message.AgentMessage) {
	if r.cfg.HistoryStore == nil {
		return
	}
	_ = r.cfg.HistoryStore.Append(ctx, msg)
}

func (r *Runner) applyCompaction(ctx context.Context, messages []message.AgentMessage) (string, []message.AgentMessage, error) {
	if r.cfg.Budget == nil {
		return "", messages, nil
	}
	r.emit(events.Event{Type: events.ContextCompactionStart})

	compacted, summary, changed, err := message.CompactIfNeeded(ctx, *r.cfg.Budget, messages)
	if err != nil {
		return "", messages, err
	}
	if !changed {
		return "", messages, nil
	}

	r.emit(events.Event{Type: events.ContextCompactionEnd, Content: summary})

	if r.cfg.HistoryStore != nil {
		if rewriter, ok := r.cfg.HistoryStore.(history.Rewriter); ok {
			_ = rewriter.Replace(ctx, compacted)
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
