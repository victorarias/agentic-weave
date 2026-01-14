package anthropic

import "github.com/victorarias/agentic-weave/adapters"

// Adapter is a stub for Anthropic-specific capabilities.
type Adapter struct{}

func (Adapter) Capabilities() adapters.Capabilities {
	return adapters.Capabilities{
		ToolUse:        true,
		ToolChoiceNone: true,
		ToolSearch:     true,
		ToolExamples:   true,
		DeferLoad:      true,
		AllowedCallers: true,
		PromptCaching:  true,
		TokenCounting:  true,
		Batching:       true,
		ModelsAPI:      true,
		Vision:         true,
		CodeExecution:  true,
		ComputerUse:    true,
	}
}
