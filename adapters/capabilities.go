package adapters

// Capabilities describe optional adapter features.
type Capabilities struct {
	ToolUse        bool
	ToolChoiceNone bool
	ToolSearch     bool
	ToolExamples   bool
	DeferLoad      bool
	AllowedCallers bool
	PromptCaching  bool
	TokenCounting  bool
	Batching       bool
	ModelsAPI      bool
	Vision         bool
	CodeExecution  bool
	ComputerUse    bool
}

// ToolChoice represents provider tool-choice modes.
type ToolChoice string

const (
	ToolChoiceAuto ToolChoice = "auto"
	ToolChoiceNone ToolChoice = "none"
	ToolChoiceTool ToolChoice = "tool"
)
