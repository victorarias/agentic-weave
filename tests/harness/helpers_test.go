package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/victorarias/agentic-weave/agentic"
	"github.com/victorarias/agentic-weave/agentic/context/budget"
	"github.com/victorarias/agentic-weave/agentic/events"
	"github.com/victorarias/agentic-weave/agentic/loop"
)

type eventRecorder struct {
	events []events.Event
}

func (r *eventRecorder) Sink() events.Sink {
	return events.SinkFunc(func(e events.Event) {
		r.events = append(r.events, e)
	})
}

func runScenario(t *testing.T, cfg loop.Config, req loop.Request) (loop.Result, []events.Event, error) {
	t.Helper()
	recorder := &eventRecorder{}
	cfg.Events = recorder.Sink()
	runner := loop.New(cfg)
	result, err := runner.Run(context.Background(), req)
	return result, recorder.events, err
}

type deciderFunc func(context.Context, loop.Input) (loop.Decision, error)

func (f deciderFunc) Decide(ctx context.Context, in loop.Input) (loop.Decision, error) {
	return f(ctx, in)
}

type scriptedDecider struct {
	script []loop.Decision
	inputs []loop.Input
	err    error
	idx    int
}

func (d *scriptedDecider) Decide(ctx context.Context, in loop.Input) (loop.Decision, error) {
	d.inputs = append(d.inputs, in)
	if d.err != nil {
		return loop.Decision{}, d.err
	}
	if len(d.script) == 0 {
		return loop.Decision{}, nil
	}
	if d.idx >= len(d.script) {
		return d.script[len(d.script)-1], nil
	}
	decision := d.script[d.idx]
	d.idx++
	return decision, nil
}

type staticTool struct {
	def    agentic.ToolDefinition
	output json.RawMessage
	err    error
}

func (t staticTool) Definition() agentic.ToolDefinition { return t.def }

func (t staticTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if t.err != nil {
		return agentic.ToolResult{Name: call.Name}, t.err
	}
	return agentic.ToolResult{Name: call.Name, Output: t.output}, nil
}

type executorFunc struct {
	listFn func(ctx context.Context) ([]agentic.ToolDefinition, error)
	execFn func(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
}

func (e executorFunc) ListTools(ctx context.Context) ([]agentic.ToolDefinition, error) {
	if e.listFn == nil {
		return nil, nil
	}
	return e.listFn(ctx)
}

func (e executorFunc) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if e.execFn == nil {
		return agentic.ToolResult{}, nil
	}
	return e.execFn(ctx, call)
}

type appendOnlyStore struct {
	messages []budget.Message
}

func (s *appendOnlyStore) Append(ctx context.Context, msg budget.Message) error {
	s.messages = append(s.messages, msg)
	return nil
}

func (s *appendOnlyStore) Load(ctx context.Context) ([]budget.Message, error) {
	out := make([]budget.Message, len(s.messages))
	copy(out, s.messages)
	return out, nil
}
