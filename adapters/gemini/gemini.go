package gemini

import "github.com/victorarias/agentic-weave/adapters"

// Adapter is a stub for Gemini-specific capabilities.
type Adapter struct{}

func (Adapter) Capabilities() adapters.Capabilities {
	return adapters.Capabilities{
		ToolUse:        true,
		ToolChoiceNone: true,
		ToolSearch:     false,
		ToolExamples:   false,
		DeferLoad:      false,
		AllowedCallers: false,
		PromptCaching:  false,
		TokenCounting:  true,
		Batching:       true,
		ModelsAPI:      true,
		Vision:         true,
		CodeExecution:  false,
		ComputerUse:    false,
	}
}
