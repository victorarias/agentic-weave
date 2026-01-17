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
type Event struct {
	Type       string
	MessageID  string
	Role       string
	Content    string
	Delta      string
	ToolCall   *agentic.ToolCall
	ToolResult *agentic.ToolResult
}

// Sink consumes events (streaming, logging, UI).
type Sink interface {
	Emit(Event)
}

// SinkFunc adapts a function to a Sink.
type SinkFunc func(Event)

func (f SinkFunc) Emit(e Event) { f(e) }
