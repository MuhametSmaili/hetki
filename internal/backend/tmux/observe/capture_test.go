package observe

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func captureFixtureReply(t *testing.T, args []string, rows string) string {
	t.Helper()
	var out strings.Builder
	out.WriteString(testInventoryOutput(""))
	for n, arg := range args {
		switch {
		case arg == "display-message" && strings.HasPrefix(args[n+2], "hetki-capture-"):
			out.WriteString(args[n+2] + "\n")
		case arg == "capture-pane":
			first, err := strconv.Atoi(args[n+4])
			require.NoError(t, err)
			last, err := strconv.Atoi(args[n+6])
			require.NoError(t, err)
			out.WriteString(strings.Repeat(args[n+8]+" content\n", last-first+1))
		}
	}
	out.WriteString(testInventoryOutput(rows))
	return out.String()
}

func TestCaptureBatchesMixedTargetsAndRetainsRevision(t *testing.T) {
	rows := testPaneRow + "$1|@2|%2|101|0|/dev/pts/2|other-app|80|30|0\n"
	queries, captures := 0, 0
	s := testSampler(t, func(_ context.Context, limit int, args ...string) (string, error) {
		if slices.Contains(args, "capture-pane") {
			captures++
			require.Equal(t, []string{"-S", "/tmp/fixture.sock"}, args[:2])
			require.Equal(t, maxCaptureBatchBytes, limit)
			require.False(t, slices.Contains(args, "-e"))
			require.False(t, slices.Contains(args, "-a"))
			return captureFixtureReply(t, args, rows), nil
		}
		queries++
		return testInventoryOutput(rows), nil
	})
	inventory, err := s.Inventory(t.Context(), 5)
	require.NoError(t, err)
	first, err := inventory.Target("%1", Region{Bottom: true, Rows: 2})
	require.NoError(t, err)
	second, err := inventory.Target("%2", Region{Start: 4, Rows: 1})
	require.NoError(t, err)
	queries = 0

	results, err := s.Capture(t.Context(), []CaptureTarget{first, second})

	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Zero(t, queries, "the capture request also reads the facts")
	require.Equal(t, 1, captures)
	require.Equal(t, "%1 content\n%1 content\n", results[0].Text)
	require.Equal(t, "%2 content\n", results[1].Text)
	for _, result := range results {
		require.NoError(t, result.Err)
		require.Equal(t, uint64(5), result.Target.Revision())
		require.False(t, result.StartedAt.IsZero())
	}
}

func TestCaptureRejectsChangedPaneFacts(t *testing.T) {
	cases := []struct {
		name string
		old  string
		new  string
	}{
		{"respawn", "|100|", "|102|"},
		{"command", "|cat|", "|other|"},
		{"width", "|80|", "|90|"},
		{"height", "|30|", "|40|"},
		{"alternate screen", "|30|0\n", "|30|1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := false
			s := testSampler(t, func(_ context.Context, _ int, args ...string) (string, error) {
				row := testPaneRow
				if changed {
					row = strings.Replace(row, tc.old, tc.new, 1)
				}
				if slices.Contains(args, "capture-pane") {
					return captureFixtureReply(t, args, row), nil
				}
				return testInventoryOutput(row), nil
			})
			inventory, err := s.Inventory(t.Context(), 1)
			require.NoError(t, err)
			target, err := inventory.Target("%1", Region{})
			require.NoError(t, err)
			changed = true

			results, err := s.Capture(t.Context(), []CaptureTarget{target})

			require.NoError(t, err)
			require.Len(t, results, 1)
			require.ErrorIs(t, results[0].Err, ErrChanged)
			require.Empty(t, results[0].Text)
		})
	}
}

func TestCapturePreservesOtherPaneWhenNativeOwnerChanges(t *testing.T) {
	rows := testPaneRow + "$1|@2|%2|101|0|/dev/pts/2|other-app|80|30|0\n"
	captured := false
	s := testSampler(t, func(_ context.Context, _ int, args ...string) (string, error) {
		if slices.Contains(args, "capture-pane") {
			captured = true
			return captureFixtureReply(t, args, rows), nil
		}
		return testInventoryOutput(rows), nil
	})
	s.process = func(ctx context.Context, pid int) (processInfo, error) {
		info, err := fixtureProcess(ctx, pid)
		if captured && pid == 100 {
			info.Birth = "replacement"
		}
		return info, err
	}
	inventory, err := s.Inventory(t.Context(), 1)
	require.NoError(t, err)
	first, err := inventory.Target("%1", Region{Rows: 1})
	require.NoError(t, err)
	second, err := inventory.Target("%2", Region{Rows: 1})
	require.NoError(t, err)

	results, err := s.Capture(t.Context(), []CaptureTarget{first, second})

	require.NoError(t, err)
	require.ErrorIs(t, results[0].Err, ErrChanged)
	require.Empty(t, results[0].Text)
	require.NoError(t, results[1].Err)
	require.Equal(t, "%2 content\n", results[1].Text)
}

func TestCaptureKeepsOtherPanesWhenOneTargetChanges(t *testing.T) {
	peer := "$1|@2|%2|101|0|/dev/pts/2|other-app|80|30|0\n"
	cases := []struct {
		name string
		old  string
		new  string
		rows int
	}{
		{"command changed", "|cat|", "|replacement|", 30},
		{"viewport shrank", "|30|", "|20|", 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captures := 0
			s := testSampler(t, func(_ context.Context, _ int, args ...string) (string, error) {
				if slices.Contains(args, "capture-pane") {
					first := strings.Replace(testPaneRow, tc.old, tc.new, 1)
					captures++
					out := captureFixtureReply(t, args, first+peer)
					return strings.Replace(out, strings.Repeat("%1 content\n", 30), strings.Repeat("%1 content\n", tc.rows), 1), nil
				}
				return testInventoryOutput(testPaneRow + peer), nil
			})
			inventory, err := s.Inventory(t.Context(), 1)
			require.NoError(t, err)
			first, err := inventory.Target("%1", Region{})
			require.NoError(t, err)
			second, err := inventory.Target("%2", Region{Rows: 1})
			require.NoError(t, err)

			results, err := s.Capture(t.Context(), []CaptureTarget{first, second})

			require.NoError(t, err)
			require.Equal(t, 1, captures)
			require.ErrorIs(t, results[0].Err, ErrChanged)
			require.Empty(t, results[0].Text)
			require.True(t, results[0].StartedAt.IsZero())
			require.NoError(t, results[1].Err)
			require.Equal(t, "%2 content\n", results[1].Text)
		})
	}
}

func TestCaptureRejectsMalformedScreensWithResizedPane(t *testing.T) {
	peer := "$1|@2|%2|101|0|/dev/pts/2|other-app|80|30|0\n"
	cases := []struct {
		name      string
		firstText string
		peerRows  int
	}{
		{"unterminated resized screen", "resized", 30},
		{"too many resized rows", strings.Repeat("row\n", maxCaptureRows+1), 30},
		{"missing unchanged peer row", strings.Repeat("row\n", 20), 29},
		{"extra unchanged peer row", strings.Repeat("row\n", 20), 31},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testSampler(t, func(_ context.Context, _ int, args ...string) (string, error) {
				if !slices.Contains(args, "capture-pane") {
					return testInventoryOutput(testPaneRow + peer), nil
				}
				resized := strings.Replace(testPaneRow, "|30|", "|20|", 1)
				out := captureFixtureReply(t, args, resized+peer)
				out = strings.Replace(out, strings.Repeat("%1 content\n", 30), tc.firstText, 1)
				out = strings.Replace(out, strings.Repeat("%2 content\n", 30), strings.Repeat("%2 content\n", tc.peerRows), 1)
				return out, nil
			})
			inventory, err := s.Inventory(t.Context(), 1)
			require.NoError(t, err)
			first, err := inventory.Target("%1", Region{})
			require.NoError(t, err)
			second, err := inventory.Target("%2", Region{})
			require.NoError(t, err)

			screens, err := s.Capture(t.Context(), []CaptureTarget{first, second})

			require.Error(t, err)
			require.Nil(t, screens)
		})
	}
}

func TestCaptureFailsOnlyAnOversizedScreen(t *testing.T) {
	peer := "$1|@2|%2|101|0|/dev/pts/2|other-app|80|30|0\n"
	s := testSampler(t, func(_ context.Context, _ int, args ...string) (string, error) {
		if !slices.Contains(args, "capture-pane") {
			return testInventoryOutput(testPaneRow + peer), nil
		}
		out := captureFixtureReply(t, args, testPaneRow+peer)
		wide := strings.Repeat(strings.Repeat("x", maxScreenBytes/30)+"\n", 30)
		return strings.Replace(out, strings.Repeat("%1 content\n", 30), wide, 1), nil
	})
	inventory, err := s.Inventory(t.Context(), 1)
	require.NoError(t, err)
	first, err := inventory.Target("%1", Region{})
	require.NoError(t, err)
	second, err := inventory.Target("%2", Region{})
	require.NoError(t, err)

	screens, err := s.Capture(t.Context(), []CaptureTarget{first, second})

	require.NoError(t, err)
	require.Len(t, screens, 2)
	require.ErrorIs(t, screens[0].Err, ErrScreenTooLarge)
	require.Empty(t, screens[0].Text)
	require.NoError(t, screens[1].Err)
	require.Equal(t, strings.Repeat("%2 content\n", 30), screens[1].Text)
}

func TestCaptureRetriesWithoutClosedPanes(t *testing.T) {
	peer := "$1|@2|%2|101|0|/dev/pts/2|other-app|80|30|0\n"
	cases := []struct {
		name      string
		remaining string
		batches   int
		captured  string
	}{
		{"peer closed", testPaneRow, 2, "%1 content\n"},
		{"all closed", "", 1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := testPaneRow + peer
			batches := 0
			s := testSampler(t, func(_ context.Context, _ int, args ...string) (string, error) {
				if !slices.Contains(args, "capture-pane") {
					return testInventoryOutput(rows), nil
				}
				batches++
				if batches == 1 {
					return "", errors.New("can't find pane: %2")
				}
				return captureFixtureReply(t, args, rows), nil
			})
			inventory, err := s.Inventory(t.Context(), 1)
			require.NoError(t, err)
			first, err := inventory.Target("%1", Region{Rows: 1})
			require.NoError(t, err)
			second, err := inventory.Target("%2", Region{Rows: 1})
			require.NoError(t, err)
			rows = tc.remaining

			results, err := s.Capture(t.Context(), []CaptureTarget{first, second})

			require.NoError(t, err)
			require.Equal(t, tc.batches, batches)
			require.Len(t, results, 2)
			require.Equal(t, tc.captured, results[0].Text)
			require.ErrorIs(t, results[1].Err, ErrChanged)
		})
	}
}

func TestCapturePropagatesBatchFailureWithoutPartialResults(t *testing.T) {
	failed := errors.New("capture failed")
	s := testSampler(t, func(_ context.Context, _ int, args ...string) (string, error) {
		if slices.Contains(args, "capture-pane") {
			return "partial screen", failed
		}
		return testInventoryOutput(testPaneRow), nil
	})
	inventory, err := s.Inventory(t.Context(), 1)
	require.NoError(t, err)
	target, err := inventory.Target("%1", Region{})
	require.NoError(t, err)

	results, err := s.Capture(t.Context(), []CaptureTarget{target})

	require.ErrorIs(t, err, failed)
	require.Nil(t, results)
}

func TestCaptureRejectsInvalidTargetsWithoutIO(t *testing.T) {
	s := testSampler(t, func(context.Context, int, ...string) (string, error) {
		t.Fatal("invalid targets must not issue a query")
		return "", nil
	})
	target := CaptureTarget{server: s.server.Identity(), revision: 1, pane: Pane{ID: "%1"}}
	cases := []struct {
		name    string
		targets []CaptureTarget
	}{
		{"zero target", []CaptureTarget{{}}},
		{"foreign server", []CaptureTarget{{server: "other", revision: 1}}},
		{"duplicate pane", []CaptureTarget{target, target}},
		{"too many targets", make([]CaptureTarget, maxCaptureTargets+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Capture(t.Context(), tc.targets)

			require.Error(t, err)
		})
	}
	results, err := s.Capture(t.Context(), nil)
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestCaptureCancellationDiscardsCollectedText(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := testSampler(t, func(_ context.Context, _ int, args ...string) (string, error) {
		if slices.Contains(args, "capture-pane") {
			out := captureFixtureReply(t, args, testPaneRow)
			cancel()
			return out, nil
		}
		return testInventoryOutput(testPaneRow), nil
	})
	inventory, err := s.Inventory(ctx, 1)
	require.NoError(t, err)
	target, err := inventory.Target("%1", Region{})
	require.NoError(t, err)

	results, err := s.Capture(ctx, []CaptureTarget{target})

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, results)
}

func TestCaptureRejectsIncompletePostCaptureFacts(t *testing.T) {
	cases := []struct {
		name  string
		facts string
	}{
		{"missing pane", testInventoryOutput("")},
		{"wrong pane", testInventoryOutput(strings.Replace(testPaneRow, "%1", "%2", 1))},
		{"duplicate pane", testInventoryOutput(testPaneRow + testPaneRow)},
		{"truncated facts", testInventoryOutput(strings.TrimSuffix(testPaneRow, "\n"))},
		{"changed server", "43|123|3.8\n" + testPaneRow},
		{"oversized facts", strings.Repeat("x", maxInventoryBytes+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testSampler(t, func(_ context.Context, _ int, args ...string) (string, error) {
				if !slices.Contains(args, "capture-pane") {
					return testInventoryOutput(testPaneRow), nil
				}
				out := captureFixtureReply(t, args, testPaneRow)
				return strings.TrimSuffix(out, testInventoryOutput(testPaneRow)) + tc.facts, nil
			})
			inventory, err := s.Inventory(t.Context(), 1)
			require.NoError(t, err)
			target, err := inventory.Target("%1", Region{Rows: 1})
			require.NoError(t, err)

			results, err := s.Capture(t.Context(), []CaptureTarget{target})

			require.Error(t, err)
			require.Nil(t, results, "valid text alone must not survive failed post-capture validation")
		})
	}
}

func TestCaptureChecksNativeLifetimesAfterCombinedBatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		pid  int
	}{
		{"server PID reused", 42},
		{"root PID reused", 100},
		{"foreground PID reused", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := captureLifetimeFixture(t, tc.pid)
			inventory, err := s.Inventory(t.Context(), 1)
			require.NoError(t, err)
			target, err := inventory.Target("%1", Region{Rows: 1})
			require.NoError(t, err)

			results, err := s.Capture(t.Context(), []CaptureTarget{target})

			if tc.pid == 42 {
				require.ErrorIs(t, err, ErrServerChanged)
				require.Nil(t, results)
			} else {
				require.NoError(t, err)
				require.ErrorIs(t, results[0].Err, ErrChanged)
				require.Empty(t, results[0].Text)
			}
		})
	}
}

func captureLifetimeFixture(t *testing.T, replacedPID int) *Sampler {
	t.Helper()
	captured := false
	s := testSampler(t, func(_ context.Context, _ int, args ...string) (string, error) {
		if slices.Contains(args, "capture-pane") {
			captured = true
			return captureFixtureReply(t, args, testPaneRow), nil
		}
		return testInventoryOutput(testPaneRow), nil
	})
	s.process = func(ctx context.Context, pid int) (processInfo, error) {
		info, err := fixtureProcess(ctx, pid)
		if pid == 100 {
			info.foreground = 200
		}
		if captured && pid == replacedPID {
			info.Birth = "replacement"
		}
		return info, err
	}
	return s
}

func TestCaptureRegions(t *testing.T) {
	cases := []struct {
		name        string
		region      Region
		height      int
		first, last int
		valid       bool
	}{
		{"whole", Region{}, 30, 0, 29, true},
		{"whole oversized", Region{}, 250, 0, 0, false},
		{"bottom tall screen", Region{Bottom: true, Rows: 3}, 250, 247, 249, true},
		{"bottom short screen", Region{Bottom: true, Rows: 50}, 30, 0, 29, true},
		{"explicit", Region{Start: 4, Rows: 2}, 30, 4, 5, true},
		{"out of viewport", Region{Start: 29, Rows: 2}, 30, 0, 0, false},
		{"scrollback", Region{Start: -1, Rows: 2}, 30, 0, 0, false},
		{"unbounded bottom", Region{Bottom: true}, 30, 0, 0, false},
		{"conflicting bottom", Region{Bottom: true, Start: 1, Rows: 2}, 30, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first, last, err := tc.region.bounds(tc.height)

			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, tc.first, first)
				require.Equal(t, tc.last, last)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestScreenFramingRejectsAmbiguousOrTruncatedText(t *testing.T) {
	pane := Pane{ID: "%1", Rows: 2}
	targets := []CaptureTarget{{pane: pane, first: 0, last: 1}}
	after := Inventory{panes: map[string]Pane{"%1": pane}}
	cases := []struct {
		name, out string
	}{
		{"missing start", "a\nb\nm-end-1\n"},
		{"wrong pane", "m-begin-2\na\nb\nm-end-2\n"},
		{"missing end", "m-begin-1\na\nb\n"},
		{"missing row", "m-begin-1\na\nm-end-1\n"},
		{"extra row", "m-begin-1\na\nb\nc\nm-end-1\n"},
		{"embedded marker", "m-begin-1\na\nmarker\nm-end-1\n"},
		{"trailing output", "m-begin-1\na\nb\nm-end-1\nextra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			texts, err := splitScreens(tc.out, "m", targets, after)

			require.Error(t, err)
			require.Nil(t, texts)
		})
	}
}

func TestCaptureBatchRejectsAmbiguousSections(t *testing.T) {
	s := testSampler(t, nil)
	targets := []CaptureTarget{{pane: Pane{ID: "%1"}, first: 0, last: 1}}
	screens := testInventoryOutput("m-begin-1\na\nb\nm-end-1\n")
	facts := "m-facts\n" + testInventoryOutput(testPaneRow)
	cases := []struct {
		name string
		out  string
	}{
		{"missing facts boundary", screens + testInventoryOutput(testPaneRow)},
		{"trailing screen data", screens + "extra\n" + facts},
		{"embedded facts marker", testInventoryOutput("m-begin-1\nm-facts\nb\nm-end-1\n") + facts},
		{"oversized screen section", strings.Repeat("x", maxCaptureBytes+1) + facts},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			texts, inventory, err := s.parseCaptureBatch(tc.out, "m", targets, []string{"%1"})

			require.Error(t, err)
			require.Nil(t, texts)
			require.Empty(t, inventory.panes)
		})
	}
}
