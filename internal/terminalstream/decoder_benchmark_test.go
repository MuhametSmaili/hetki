package terminalstream

import (
	"strings"
	"testing"
)

func BenchmarkDecoder(b *testing.B) {
	cases := []struct {
		name  string
		input string
	}{
		{"plain output", strings.Repeat("ordinary output\n", 256)},
		{"OSC report", "\x1b]7501;state=working\x1b\\"},
		{"oversized OSC", "\x1b]" + strings.Repeat("x", 8192) + "\a"},
		{"opaque DCS", "\x1bP" + strings.Repeat("x", 4096) + "\x1b\\"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var decoder Decoder
			data := []byte(tc.input)
			emit := func(Frame) {}
			decoder.Feed(data, emit)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				decoder.Feed(data, emit)
			}
		})
	}
}
