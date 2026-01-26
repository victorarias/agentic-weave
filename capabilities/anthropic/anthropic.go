package anthropic

import "github.com/victorarias/agentic-weave/capabilities"

// Adapter is a stub for Anthropic-specific capabilities.
type Adapter struct{}

func (Adapter) Capabilities() capabilities.Capabilities {
	return capabilities.Capabilities{
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
