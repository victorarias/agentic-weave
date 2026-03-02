// Package turn provides cache-friendly turn assembly primitives used by
// conversation orchestration.
package turn

import (
	"fmt"
	"strings"

	"github.com/victorarias/agentic-weave/agentic/loop"
	"github.com/victorarias/agentic-weave/agentic/message"
)

const runtimeContextHeader = "[runtime_context]\nSystem-generated context for this turn (not user-authored input)."

type namedSection struct {
	key     string
	content string
}

// Builder assembles a turn in a cache-friendly way.
//
// The system prompt is provided once and returned unchanged.
// Dynamic context is appended to the user message as runtime sections.
type Builder struct {
	systemPrompt string
	sections     []namedSection
}

// Assembled is the output of Builder.Assemble.
type Assembled struct {
	SystemPrompt string
	UserMessage  string
	SectionKeys  []string
}

// NewBuilder creates a builder with the provided system prompt.
func NewBuilder(systemPrompt string) *Builder {
	return &Builder{systemPrompt: systemPrompt}
}

// Section appends a [key]\ncontent runtime section.
//
// Contract:
//   - key is trimmed
//   - empty key is skipped
//   - content with only whitespace is skipped
//   - duplicate keys are allowed and order is preserved
func (b *Builder) Section(key, content string) *Builder {
	key = strings.TrimSpace(key)
	if key == "" {
		return b
	}
	if strings.TrimSpace(content) == "" {
		return b
	}
	formatted := fmt.Sprintf("[%s]\n%s", key, content)
	b.sections = append(b.sections, namedSection{key: key, content: formatted})
	return b
}

// Reminder appends a <system-reminder>...</system-reminder> runtime section.
//
// key is used for SectionKeys metadata and follows the same normalization as
// Section().
func (b *Builder) Reminder(key, content string) *Builder {
	key = strings.TrimSpace(key)
	if key == "" {
		return b
	}
	if strings.TrimSpace(content) == "" {
		return b
	}
	formatted := fmt.Sprintf("<system-reminder>\n%s\n</system-reminder>", content)
	b.sections = append(b.sections, namedSection{key: key, content: formatted})
	return b
}

// RawSection appends a pre-formatted runtime section as-is.
//
// Escape hatch: bypasses Section/Reminder formatting safeguards.
func (b *Builder) RawSection(key, content string) *Builder {
	key = strings.TrimSpace(key)
	if key == "" {
		return b
	}
	if strings.TrimSpace(content) == "" {
		return b
	}
	b.sections = append(b.sections, namedSection{key: key, content: content})
	return b
}

// Assemble builds the final user message.
//
// If no sections were added, the user message is returned unchanged.
func (b *Builder) Assemble(userMessage string) Assembled {
	keys := make([]string, 0, len(b.sections))
	if len(b.sections) == 0 {
		return Assembled{
			SystemPrompt: b.systemPrompt,
			UserMessage:  userMessage,
			SectionKeys:  keys,
		}
	}

	sectionBodies := make([]string, 0, len(b.sections))
	for _, section := range b.sections {
		keys = append(keys, section.key)
		sectionBodies = append(sectionBodies, section.content)
	}

	var sb strings.Builder
	if strings.TrimSpace(userMessage) != "" {
		sb.WriteString(userMessage)
		sb.WriteString("\n\n")
	}
	sb.WriteString(runtimeContextHeader)
	sb.WriteString("\n\n")
	sb.WriteString(strings.Join(sectionBodies, "\n\n"))

	return Assembled{
		SystemPrompt: b.systemPrompt,
		UserMessage:  sb.String(),
		SectionKeys:  keys,
	}
}

// Request is a convenience around Assemble() for loop.Run().
func (b *Builder) Request(userMessage string, history []message.AgentMessage) loop.Request {
	assembled := b.Assemble(userMessage)
	return loop.Request{
		SystemPrompt: assembled.SystemPrompt,
		UserMessage:  assembled.UserMessage,
		History:      history,
	}
}
