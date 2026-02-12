package sanitize

import "testing"

func TestTextStripsTerminalControlBytes(t *testing.T) {
	input := "ok\x1b[31mred\x1b[0m\x07\x00\t\n"
	got := Text(input)
	if got != "ok[31mred[0m\t\n" {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}
