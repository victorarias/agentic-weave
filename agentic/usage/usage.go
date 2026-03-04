package usage

// Usage captures token usage for a single model response.
type Usage struct {
	Input              int
	Output             int
	Total              int
	CacheReadInput     int
	CacheCreationInput int
}

// Add combines two Usage values by summing all token counts.
// If b is nil, a is returned unchanged.
func Add(a Usage, b *Usage) Usage {
	if b == nil {
		return a
	}
	return Normalize(Usage{
		Input:              a.Input + b.Input,
		Output:             a.Output + b.Output,
		CacheReadInput:     a.CacheReadInput + b.CacheReadInput,
		CacheCreationInput: a.CacheCreationInput + b.CacheCreationInput,
	})
}

// StopReason describes why a generation stopped.
type StopReason string

const (
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonStop      StopReason = "stop"
	StopReasonTool      StopReason = "tool"
	StopReasonError     StopReason = "error"
	StopReasonAbort     StopReason = "abort"
)

// Normalize fills Total when missing.
func Normalize(u Usage) Usage {
	if u.Total == 0 {
		u.Total = u.Input + u.Output
	}
	return u
}
