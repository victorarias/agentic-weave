package capabilities

import (
	"strings"

	"github.com/victorarias/agentic-weave/agentic/usage"
)

// StopReasonFromFinish maps provider finish reasons to normalized stop reasons.
func StopReasonFromFinish(reason string) usage.StopReason {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch r {
	case "length", "max_tokens", "max tokens", "max_output_tokens", "max_output", "max_completion_tokens", "max_tokens_exceeded", "max_output_exceeded", "context_length_exceeded":
		return usage.StopReasonMaxTokens
	case "stop", "end_turn", "end", "eos", "complete":
		return usage.StopReasonStop
	case "tool_calls", "tool_use", "function_call", "tool":
		return usage.StopReasonTool
	case "abort", "aborted", "cancelled", "canceled":
		return usage.StopReasonAbort
	case "error", "failed":
		return usage.StopReasonError
	default:
		return ""
	}
}

// NormalizeUsage ensures total tokens are populated.
func NormalizeUsage(input, output, total int) usage.Usage {
	return usage.Normalize(usage.Usage{Input: input, Output: output, Total: total})
}
