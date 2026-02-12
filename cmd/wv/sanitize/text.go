package sanitize

// Text removes terminal control bytes from untrusted text while preserving
// printable content and common whitespace.
func Text(value string) string {
	out := make([]rune, 0, len(value))
	for _, r := range value {
		switch r {
		case '\n', '\r', '\t':
			out = append(out, r)
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		if r >= 0x80 && r <= 0x9f {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
