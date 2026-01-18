package usage

// Usage captures token usage for a single model response.
type Usage struct {
	Input  int
	Output int
	Total  int
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
