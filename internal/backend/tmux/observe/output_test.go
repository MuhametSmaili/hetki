package observe

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaneOutputDecodesBytesWithoutTerminalInterpretation(t *testing.T) {
	line := []byte(`%12 \033]7501;state=working\007日本語\134\000`)

	output, err := parsePaneOutput(line)

	require.NoError(t, err)
	require.Equal(t, "%12", output.Pane)
	require.Equal(t, []byte("\x1b]7501;state=working\x07日本語\\\x00"), output.Data)
}

func TestMalformedControlOutputNeverReachesConsumer(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"truncated escape", `%output %1 value\03`},
		{"non octal escape", `%output %1 value\08a`},
		{"overflow escape", `%output %1 value\777`},
		{"missing pane", `%output %1`},
		{"invalid pane", `%output name value`},
		{"paused", `%pause %1`},
		{"resumed with gap", `%continue %1`},
		{"changed session", `%session-changed $2 other`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connection := &Connection{session: "$1"}
			called := false

			err := connection.deliverLine([]byte(tc.line), func(PaneOutput) error {
				called = true
				return nil
			})

			require.ErrorIs(t, err, ErrStreamLost)
			require.False(t, called)
		})
	}
}

func TestBoundedControlLinesRejectTruncationAndOverflow(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  error
	}{
		{"complete", "%output %1 value\n", nil},
		{"truncated", "%output %1 value", io.ErrUnexpectedEOF},
		{"oversized", strings.Repeat("x", maxControlLine) + "\n", bufio.ErrTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(tc.input))
			scanner.Buffer(make([]byte, 4096), maxControlLine)
			scanner.Split(splitControlLine)
			connection := &Connection{reader: scanner}

			_, err := connection.readLine()

			require.ErrorIs(t, err, tc.want)
		})
	}
}

func FuzzControlOutput(f *testing.F) {
	f.Add(`%1 hello\033]7501;state=working\007`)
	f.Add(`%2 \777`)
	f.Fuzz(func(t *testing.T, line string) {
		if len(line) > maxControlLine {
			t.Skip()
		}

		output, err := parsePaneOutput([]byte(line))
		if err == nil && len(output.Data) > len(line) {
			t.Fatal("decoded output grew beyond its bounded input")
		}
	})
}

func BenchmarkControlDecode(b *testing.B) {
	input := []byte(`%1 \033]7501;state=working\007`)
	buffer := make([]byte, len(input))
	b.ReportAllocs()

	for b.Loop() {
		copy(buffer, input)
		if _, err := parsePaneOutput(buffer); err != nil {
			b.Fatal(err)
		}
	}
}
