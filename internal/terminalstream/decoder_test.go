package terminalstream

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type observation struct {
	kind Kind
	body string
}

func collect(chunks ...string) []observation {
	var decoder Decoder
	var frames []observation
	emit := func(frame Frame) {
		frames = append(frames, observation{kind: frame.Kind, body: string(frame.Body)})
	}
	for _, chunk := range chunks {
		decoder.Feed([]byte(chunk), emit)
	}
	return frames
}

func TestFramingAcrossEverySplitPoint(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []observation
	}{
		{
			name:  "reports and prompt in one stream",
			input: "hello 🌍\x1b]7501;state=working\a\x1b]133;A\x1b\\",
			want: []observation{
				{kind: OSC, body: "7501;state=working"},
				{kind: OSC, body: "133;A"},
			},
		},
		{
			name:  "preserve arbitrary OSC payload bytes",
			input: "\x1b]2;héllo\x00\x7f\xc2\x9c\x1b\\",
			want:  []observation{{kind: OSC, body: "2;héllo\x00\x7f\xc2\x9c"}},
		},
		{
			name:  "reset with ignored controls",
			input: "\x1b\x00\x7fc",
			want:  []observation{{kind: RIS}},
		},
		{
			name:  "real reset after unrelated escape",
			input: "\x1b(B\x1bc",
			want:  []observation{{kind: RIS}},
		},
		{
			name:  "intermediates are not reset or OSC",
			input: "\x1b(c\x1b$)c\x1b ]7501;state=idle\a",
		},
		{
			name:  "soft reset and alternate screen",
			input: "\x1b[!p\x1b[?1049h\x1b[?1049l",
		},
		{
			name:  "CAN aborts OSC",
			input: "\x1b]7501;state=idle\x18ignored\a",
		},
		{
			name:  "SUB aborts pending terminator",
			input: "\x1b]7501;state=idle\x1b\x1a\\",
		},
		{
			name:  "escape aborts OSC and restarts",
			input: "\x1b]incomplete\x1b]7501;state=blocked\a",
			want:  []observation{{kind: OSC, body: "7501;state=blocked"}},
		},
		{
			name:  "escape aborts OSC into RIS",
			input: "\x1b]incomplete\x1bc",
			want:  []observation{{kind: RIS}},
		},
		{
			name:  "intermediate after aborted OSC is not RIS",
			input: "\x1b]incomplete\x1b(c",
		},
		{
			name:  "escape restarts CSI",
			input: "\x1b[123\x1b]133;C\a",
			want:  []observation{{kind: OSC, body: "133;C"}},
		},
		{
			name:  "eight bit introducer is not supported",
			input: "\x9d7501;state=idle\x9c",
		},
		{
			name:  "unterminated OSC emits nothing",
			input: "\x1b]7501;state=working\x1b",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for split := 0; split <= len(tc.input); split++ {
				got := collect(tc.input[:split], tc.input[split:])

				require.Equal(t, tc.want, got, "split at byte %d", split)
			}

			chunks := strings.Split(tc.input, "")
			require.Equal(t, tc.want, collect(chunks...))
		})
	}
}

func TestDCSDoesNotExposeReportsOrResets(t *testing.T) {
	input := "\x1bPpayload\x1bc\x1b]7501;state=idle\a\x1b\\"
	input += "\x1b]133;A\a"
	want := []observation{{kind: OSC, body: "133;A"}}

	for split := 0; split <= len(input); split++ {
		got := collect(input[:split], input[split:])

		require.Equal(t, want, got, "split at byte %d", split)
	}
}

// tmux ends SOS, PM and APC at any ESC, so an unterminated string cannot hide
// the reports and resets that follow it.
func TestEscapeEndsUnterminatedControlStrings(t *testing.T) {
	for _, introducer := range []string{"X", "^", "_"} {
		t.Run(introducer, func(t *testing.T) {
			input := "\x1b" + introducer + "payload\x1bc"
			input += "\x1b" + introducer + "payload\x1b\x1b]7501;state=idle\a"
			input += "\x1b" + introducer + "payload\x1b\\\x1b]133;A\a"
			want := []observation{{kind: RIS}, {kind: OSC, body: "7501;state=idle"}, {kind: OSC, body: "133;A"}}

			for split := 0; split <= len(input); split++ {
				got := collect(input[:split], input[split:])

				require.Equal(t, want, got, "split at byte %d", split)
			}
		})
	}
}

func TestDoubledEscapesInWrappedStringsRemainOpaque(t *testing.T) {
	wrapped := "\x1bPtmux;\x1b\x1b]7501;state=working\x1b\x1b\\"
	wrapped += "\x1b\x1b]7501;state=idle\x1b\x1b\\\x1b\\"
	input := wrapped + "\x1b]133;A\a"
	want := []observation{{kind: OSC, body: "133;A"}}

	for split := 0; split <= len(input); split++ {
		got := collect(input[:split], input[split:])

		require.Equal(t, want, got, "split at byte %d", split)
	}
}

func TestWholeSequenceLimitCountsBothTerminators(t *testing.T) {
	for _, ending := range []string{"\a", "\x1b\\"} {
		for _, size := range []int{MaxSequenceBytes - 1, MaxSequenceBytes, MaxSequenceBytes + 1} {
			t.Run(fmt.Sprintf("terminator_%d_bytes/size_%d", len(ending), size), func(t *testing.T) {
				body := strings.Repeat("x", size-2-len(ending))
				input := "\x1b]" + body + ending
				want := []observation{{kind: OSC, body: "133;A"}}
				if size <= MaxSequenceBytes {
					want = append([]observation{{kind: OSC, body: body}}, want...)
				}

				got := collect(input[:len(input)-1], input[len(input)-1:], "\x1b]133;A\a")

				require.Equal(t, want, got)
			})
		}
	}
}

func TestOversizedUnterminatedInputStaysBoundedAndRecovers(t *testing.T) {
	var decoder Decoder
	var frames []observation
	emit := func(frame Frame) {
		frames = append(frames, observation{kind: frame.Kind, body: string(frame.Body)})
	}
	decoder.Feed([]byte("\x1b]"), emit)
	block := []byte(strings.Repeat("x", 4096))

	for range 256 {
		decoder.Feed(block, emit)
	}
	retained, capacity, length := len(decoder.body), cap(decoder.body), decoder.length
	decoder.Feed([]byte("\x1b]133;A\a"), emit)

	require.LessOrEqual(t, retained, MaxSequenceBytes-3)
	require.LessOrEqual(t, capacity, MaxSequenceBytes-3)
	require.Equal(t, MaxSequenceBytes+1, length)
	require.Equal(t, []observation{{kind: OSC, body: "133;A"}}, frames)
}

func TestResetPreventsJoiningDifferentInputLifetimes(t *testing.T) {
	var decoder Decoder
	var frames []observation
	emit := func(frame Frame) {
		frames = append(frames, observation{kind: frame.Kind, body: string(frame.Body)})
	}
	decoder.Feed([]byte("\x1b]7501;state=work"), emit)

	decoder.Reset()
	decoder.Feed([]byte("ing\a"), emit)
	decoder.Feed([]byte("\x1b]133;A\a"), emit)

	require.Equal(t, []observation{{kind: OSC, body: "133;A"}}, frames)
}
