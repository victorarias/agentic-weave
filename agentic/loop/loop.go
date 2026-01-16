package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/history"
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
	History      []budget.Message
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
	History      []budget.Message
}

// Result captures the final output.
type Result struct {
	Reply       string
	History     []budget.Message
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

// Run executes the loop for a single user request.
func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	if r.cfg.Decider == nil {
		return Result{}, errors.New("loop: decider is required")
	}
	if err := r.validateConfig(); err != nil {
		return Result{}, err
	}

	emit := r.cfg.Events
	if emit != nil {
		emit.Emit(events.Event{Type: events.AgentStart})
		defer emit.Emit(events.Event{Type: events.AgentEnd})
		emit.Emit(events.Event{Type: events.TurnStart})
		defer emit.Emit(events.Event{Type: events.TurnEnd})
	}

	historyMessages, err := r.loadHistory(ctx, req)
	if err != nil {
		return Result{}, err
	}

	userMessage := strings.TrimSpace(req.UserMessage)
	if userMessage != "" {
		historyMessages = append(historyMessages, budget.Message{Role: "user", Content: userMessage})
	}

	summary, historyMessages, err := r.applyCompaction(ctx, historyMessages)
	if err != nil {
		return Result{}, err
	}

	tools, err := r.listTools(ctx)
	if err != nil {
		return Result{}, err
	}

	toolCalls := make([]agentic.ToolCall, 0)
	toolResults := make([]agentic.ToolResult, 0)

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
			historyMessages = append(historyMessages, budget.Message{Role: "assistant", Content: reply})
			r.appendHistory(ctx, budget.Message{Role: "assistant", Content: reply})
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

		for i, call := range decision.ToolCalls {
			if call.ID == "" {
				call.ID = fmt.Sprintf("call-%d-%d", turn, i)
			}
			if call.Caller == nil {
				call.Caller = &agentic.ToolCaller{Type: r.cfg.ToolCallerType}
			}
			if emit != nil {
				emit.Emit(events.Event{Type: events.ToolStart, ToolCall: &call})
			}

			result, err := r.cfg.Executor.Execute(ctx, call)
			if err != nil {
				result = agentic.ToolResult{
					ID:    call.ID,
					Name:  call.Name,
					Error: &agentic.ToolError{Message: err.Error()},
				}
			}

			if r.cfg.Truncation != nil {
				before := result
				trunc := truncate.Result{}
				result, trunc = truncateToolResult(result, r.cfg.TruncationMode, *r.cfg.Truncation)
				if trunc.Truncated && emit != nil {
					emit.Emit(events.Event{
						Type:       events.ToolOutputTruncated,
						ToolResult: &before,
						Content:    truncSummary(trunc),
					})
				}
			}

			if emit != nil {
				emit.Emit(events.Event{Type: events.ToolEnd, ToolResult: &result})
			}

			toolCalls = append(toolCalls, call)
			toolResults = append(toolResults, result)
			historyMessages = append(historyMessages, toolResultMessage(result))
			r.appendHistory(ctx, toolResultMessage(result))
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

func (r *Runner) loadHistory(ctx context.Context, req Request) ([]budget.Message, error) {
	if r.cfg.HistoryStore == nil {
		return append([]budget.Message(nil), req.History...), nil
	}
	return r.cfg.HistoryStore.Load(ctx)
}

func (r *Runner) appendHistory(ctx context.Context, msg budget.Message) {
	if r.cfg.HistoryStore == nil {
		return
	}
	_ = r.cfg.HistoryStore.Append(ctx, msg)
}

func (r *Runner) applyCompaction(ctx context.Context, messages []budget.Message) (string, []budget.Message, error) {
	if r.cfg.Budget == nil {
		return "", messages, nil
	}
	if r.cfg.Events != nil {
		r.cfg.Events.Emit(events.Event{Type: events.ContextCompactionStart})
	}

	compacted, summary, changed, err := r.cfg.Budget.CompactIfNeeded(ctx, messages)
	if err != nil {
		return "", messages, err
	}
	if !changed {
		return "", messages, nil
	}

	if r.cfg.Events != nil {
		r.cfg.Events.Emit(events.Event{Type: events.ContextCompactionEnd, Content: summary})
	}

	if r.cfg.HistoryStore != nil {
		if rewriter, ok := r.cfg.HistoryStore.(history.Rewriter); ok {
			_ = rewriter.Replace(ctx, compacted)
		}
	}

	return summary, compacted, nil
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

func toolResultMessage(result agentic.ToolResult) budget.Message {
	if result.Error != nil {
		return budget.Message{Role: "tool", Content: fmt.Sprintf("[tool_result] %s error: %s", result.Name, result.Error.Message)}
	}
	if len(result.Output) == 0 {
		return budget.Message{Role: "tool", Content: fmt.Sprintf("[tool_result] %s: (no output)", result.Name)}
	}
	return budget.Message{Role: "tool", Content: fmt.Sprintf("[tool_result] %s: %s", result.Name, string(result.Output))}
}
