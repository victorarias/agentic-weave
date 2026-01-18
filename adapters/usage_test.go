package adapters

import "testing"

func TestStopReasonFromFinish(t *testing.T) {
	tests := map[string]string{
		"length":            "max_tokens",
		"MAX_TOKENS":        "max_tokens",
		"max_output_tokens": "max_tokens",
		"stop":              "stop",
		"end_turn":          "stop",
		"tool_calls":        "tool",
		"function_call":     "tool",
		"abort":             "abort",
		"cancelled":         "abort",
		"error":             "error",
		"unknown_reason":    "",
	}

	for input, expected := range tests {
		got := StopReasonFromFinish(input)
		if string(got) != expected {
			t.Fatalf("expected %q -> %q, got %q", input, expected, got)
		}
	}
}

func TestNormalizeUsage(t *testing.T) {
	u := NormalizeUsage(1, 2, 0)
	if u.Total != 3 {
		t.Fatalf("expected total to be 3, got %d", u.Total)
	}
}
