package events

import "github.com/victorarias/agentic-weave/agentic"

const (
	AgentStart             = "agent_start"
	AgentEnd               = "agent_end"
	TurnStart              = "turn_start"
	TurnEnd                = "turn_end"
	MessageStart           = "message_start"
	MessageUpdate          = "message_update"
	MessageEnd             = "message_end"
	ToolStart              = "tool_execution_start"
	ToolEnd                = "tool_execution_end"
	ContextCompactionStart = "context_compaction_start"
	ContextCompactionEnd   = "context_compaction_end"
	ToolOutputTruncated    = "tool_output_truncated"
)

// Event captures a simple agent lifecycle update.
//
// Field usage varies by event type:
//   - ToolStart/ToolEnd: ToolCall contains the single tool being executed
//   - MessageEnd: ToolCalls contains all tool calls in the assistant message (may be empty)
//   - ToolOutputTruncated: ToolResult contains the pre-truncation result, Content has summary
//   - ContextCompactionEnd: Content contains the compaction summary
type Event struct {
	Type       string
	MessageID  string
	Role       string
	Content    string
	Delta      string
	ToolCall   *agentic.ToolCall   // single tool call (ToolStart/ToolEnd events)
	ToolCalls  []agentic.ToolCall  // all tool calls in message (MessageEnd events)
	ToolResult *agentic.ToolResult // tool execution result or pre-truncation data
}

// Sink consumes events (streaming, logging, UI).
type Sink interface {
	Emit(Event)
}

// SinkFunc adapts a function to a Sink.
type SinkFunc func(Event)

func (f SinkFunc) Emit(e Event) { f(e) }
