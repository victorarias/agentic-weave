package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
	agenticcontext "github.com/victorarias/agentic-weave/agentic/context"
	"github.com/victorarias/agentic-weave/agentic/events"
)

type AddTool struct{}

type AddInput struct {
	A int `json:"a"`
	B int `json:"b"`
}

type AddOutput struct {
	Sum int `json:"sum"`
}

func (AddTool) Definition() agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        "add",
		Description: "Adds two integers.",
		Examples: []agentic.ToolExample{
			{
				Description: "Simple addition",
				Input:       mustJSON(AddInput{A: 2, B: 3}),
				Output:      mustJSON(AddOutput{Sum: 5}),
			},
		},
		AllowedCallers: []string{"programmatic"},
	}
}

func (AddTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	var input AddInput
	if err := json.Unmarshal(call.Input, &input); err != nil {
		return agentic.ToolResult{ID: call.ID, Name: call.Name, Error: &agentic.ToolError{Message: err.Error()}}, nil
	}
	output := AddOutput{Sum: input.A + input.B}
	return agentic.ToolResult{ID: call.ID, Name: call.Name, Output: mustJSON(output)}, nil
}

// Agent is a tiny rule-based agent that can call tools and stream events.
type Agent struct {
	exec         agentic.ToolExecutor
	systemPrompt string
	messages     []agenticcontext.Message
}

func NewAgent(exec agentic.ToolExecutor, systemPrompt string) *Agent {
	return &Agent{exec: exec, systemPrompt: systemPrompt}
}

// StreamMessage takes a user message, decides on tool usage, and streams events.
func (a *Agent) StreamMessage(ctx context.Context, message string, sink events.Sink) (string, error) {
	sink.Emit(events.Event{Type: events.AgentStart})
	defer sink.Emit(events.Event{Type: events.AgentEnd})

	sink.Emit(events.Event{Type: events.TurnStart})
	defer sink.Emit(events.Event{Type: events.TurnEnd})

	message = strings.TrimSpace(message)
	if message == "" {
		reply := "Say something like: add 10 and 32"
		emitAssistantMessage(sink, "assistant-1", reply)
		return reply, nil
	}

	a.messages = append(a.messages, agenticcontext.Message{Role: "user", Content: message})

	if call, ok := parseAddRequest(message); ok {
		sink.Emit(events.Event{Type: events.ToolStart, ToolCall: &call})
		result, err := a.exec.Execute(ctx, call)
		sink.Emit(events.Event{Type: events.ToolEnd, ToolResult: &result})
		if err != nil {
			reply := "Sorry, I couldn't run that tool."
			emitAssistantMessage(sink, "assistant-1", reply)
			return reply, err
		}
		if result.Error != nil {
			reply := "The tool reported an error: " + result.Error.Message
			emitAssistantMessage(sink, "assistant-1", reply)
			return reply, nil
		}
		var output AddOutput
		if err := json.Unmarshal(result.Output, &output); err != nil {
			reply := "The tool returned invalid output."
			emitAssistantMessage(sink, "assistant-1", reply)
			return reply, err
		}
		reply := fmt.Sprintf("The sum is %d.", output.Sum)
		a.messages = append(a.messages, agenticcontext.Message{Role: "assistant", Content: reply})
		emitAssistantMessage(sink, "assistant-1", reply)
		return reply, nil
	}

	reply := "I can help with addition. Try: add 10 and 32"
	a.messages = append(a.messages, agenticcontext.Message{Role: "assistant", Content: reply})
	emitAssistantMessage(sink, "assistant-1", reply)
	return reply, nil
}

// BuildPrompt demonstrates how to keep the system prompt safe during compaction.
func (a *Agent) BuildPrompt(ctx context.Context, mgr agenticcontext.Manager) ([]agenticcontext.Message, string, error) {
	system := agenticcontext.Message{Role: "system", Content: a.systemPrompt}
	return agenticcontext.CompactWithSystem(ctx, system, a.messages, mgr)
}

func emitAssistantMessage(sink events.Sink, messageID, content string) {
	sink.Emit(events.Event{Type: events.MessageStart, MessageID: messageID, Role: "assistant"})
	for _, chunk := range []string{content[:len(content)/2], content[len(content)/2:]} {
		sink.Emit(events.Event{Type: events.MessageUpdate, MessageID: messageID, Role: "assistant", Delta: chunk})
	}
	sink.Emit(events.Event{Type: events.MessageEnd, MessageID: messageID, Role: "assistant", Content: content})
}

func main() {
	ctx := context.Background()
	reg := agentic.NewRegistry()
	reg.Register(AddTool{})

	agent := NewAgent(reg, "You are a helpful assistant.")

	_, err := agent.StreamMessage(ctx, "add 10 and 32", events.SinkFunc(func(e events.Event) {
		switch e.Type {
		case events.MessageUpdate:
			fmt.Print(e.Delta)
		case events.MessageEnd:
			fmt.Println()
		}
	}))
	if err != nil {
		log.Fatal(err)
	}

	_, _, _ = agent.BuildPrompt(ctx, agenticcontext.Manager{})
}

func parseAddRequest(message string) (agentic.ToolCall, bool) {
	re := regexp.MustCompile(`(?i)add\s+(-?\d+)\s*(and|\+)\s*(-?\d+)`)
	matches := re.FindStringSubmatch(message)
	if len(matches) != 4 {
		return agentic.ToolCall{}, false
	}
	a, errA := strconv.Atoi(matches[1])
	b, errB := strconv.Atoi(matches[3])
	if errA != nil || errB != nil {
		return agentic.ToolCall{}, false
	}
	return agentic.ToolCall{
		ID:     "call-1",
		Name:   "add",
		Input:  mustJSON(AddInput{A: a, B: b}),
		Caller: &agentic.ToolCaller{Type: "programmatic"},
	}, true
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
