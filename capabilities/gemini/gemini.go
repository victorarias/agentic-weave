package gemini

import "github.com/victorarias/agentic-weave/capabilities"

// Adapter is a stub for Gemini-specific capabilities.
type Adapter struct{}

func (Adapter) Capabilities() capabilities.Capabilities {
	return capabilities.Capabilities{
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
