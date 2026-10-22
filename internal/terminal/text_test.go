package terminal

import (
	"strings"
	"testing"
)

func TestSafeTerminalText(t *testing.T) {
	input := "\x1b]0;owned\a\x1b[31m中👨‍👩‍👧e\u0301\x1b[0m\n\t\x00"
	got := Sanitize(input)
	if strings.ContainsAny(got, "\x00\x1b\n\t") {
		t.Fatalf("Sanitize left terminal controls in %q", got)
	}
	if got != "中👨‍👩‍👧é\\n\\t\\x00" {
		t.Fatalf("Sanitize() = %q", got)
	}

	for width := range 13 {
		truncated := Truncate(got, width)
		if actual := Width(truncated); actual > width {
			t.Fatalf("Truncate width %d produced %q at width %d", width, truncated, actual)
		}
	}
}
