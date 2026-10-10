package screen_test

import (
	"strings"
	"testing"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/screen"
	"github.com/stretchr/testify/require"
)

func fixtureRules() screen.Config {
	return screen.Config{
		LastNonblankRows: 4,
		Rules: []screen.Rule{
			{
				State: status.Blocked,
				All:   []screen.Matcher{{Contains: "approval"}, {Contains: "choices"}},
				Any:   []screen.Matcher{{Contains: "yes"}, {Contains: "no"}},
				None:  []screen.Matcher{{Contains: "dismissed"}},
			},
			{State: status.Working, Any: []screen.Matcher{{Contains: "thinking"}, {Contains: "running"}}},
			{State: status.Working, All: []screen.Matcher{{Contains: "running"}}},
			{State: status.Idle, All: []screen.Matcher{{Regex: `(?m)^ready\z`}}},
		},
	}
}

func TestEvaluateRequiresUnambiguousPositiveEvidence(t *testing.T) {
	evaluator, err := screen.Compile(fixtureRules())
	require.NoError(t, err)
	cases := []struct {
		name string
		text string
		want status.State
	}{
		{"all and any", "approval choices yes", status.Blocked},
		{"other any match", "approval choices no", status.Blocked},
		{"missing all", "approval yes", status.Unknown},
		{"missing any", "approval choices", status.Unknown},
		{"excluded", "approval choices yes dismissed", status.Unknown},
		{"any without all", "thinking", status.Working},
		{"same state overlap", "running", status.Working},
		{"conflicting states", "approval choices yes thinking", status.Unknown},
		{"late conflicting rule", "running\nready", status.Unknown},
		{"regex", "some output\nready", status.Idle},
		{"overlay after footer", "ready\nchoose a model", status.Unknown},
		{"no match", "unrecognized screen", status.Unknown},
		{"empty", "", status.Unknown},
		{"blank", " \t\r\n\n", status.Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			state, err := evaluator.Evaluate(tc.text)

			require.NoError(t, err)
			require.Equal(t, tc.want, state)
		})
	}
}

func TestEvaluateFooterCountsInternalBlankRows(t *testing.T) {
	evaluator, err := screen.Compile(screen.Config{
		LastNonblankRows: 2,
		Rules:            []screen.Rule{{State: status.Working, All: []screen.Matcher{{Contains: "working"}}}},
	})
	require.NoError(t, err)
	cases := []struct {
		name string
		text string
		want status.State
	}{
		{"short viewport", "working", status.Working},
		{"within region", "working\nfooter", status.Working},
		{"outside region", "working\nother\nfooter", status.Unknown},
		{"internal blank counts", "working\n\nfooter", status.Unknown},
		{"trailing blanks trimmed", "working\nfooter  \r\n \t\n\n", status.Working},
		{"last nonblank row", "working  \t\r\n \n", status.Working},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, err := evaluator.Evaluate(tc.text)

			require.NoError(t, err)
			require.Equal(t, tc.want, state)
		})
	}
}

func TestEvaluateRejectsInvalidWholeCapture(t *testing.T) {
	evaluator, err := screen.Compile(fixtureRules())
	require.NoError(t, err)
	for name, text := range map[string]string{
		"oversized":      strings.Repeat("x", screen.MaxScreenBytes) + "\nready",
		"escape":         "\x1b[31mready",
		"C0":             "\x00ready",
		"C1":             "\u009bready",
		"invalid UTF-8":  "\xffready",
		"outside footer": "\a" + strings.Repeat("\nplain", 5) + "\nready",
	} {
		t.Run(name, func(t *testing.T) {
			state, err := evaluator.Evaluate(text)

			require.ErrorIs(t, err, screen.ErrInvalidScreen)
			require.Equal(t, status.Unknown, state)
		})
	}
}

func TestEvaluateAcceptsBoundedUnicodeCapture(t *testing.T) {
	evaluator, err := screen.Compile(fixtureRules())
	require.NoError(t, err)
	unicodeText := "中👨‍👩‍👧e\u0301\t\r\nready"
	text := strings.Repeat("x", screen.MaxScreenBytes-len(unicodeText)) + unicodeText

	state, err := evaluator.Evaluate(text)

	require.NoError(t, err)
	require.Equal(t, status.Idle, state)
}

func TestEvaluateUninitialized(t *testing.T) {
	for _, evaluator := range []*screen.Evaluator{nil, {}} {
		state, err := evaluator.Evaluate("ready")

		require.Error(t, err)
		require.Equal(t, status.Unknown, state)
	}
}

func BenchmarkEvaluate(b *testing.B) {
	evaluator, err := screen.Compile(fixtureRules())
	require.NoError(b, err)
	text := strings.Repeat("synthetic output\n", 196) + "approval\nchoices\nyes\n"
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()

	for b.Loop() {
		_, err := evaluator.Evaluate(text)
		if err != nil {
			b.Fatal(err)
		}
	}
}
