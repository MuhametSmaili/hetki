package statusobserver

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/screen"
	"github.com/stretchr/testify/require"
)

func redrawEvaluator(t *testing.T) *screen.Evaluator {
	t.Helper()
	evaluator, err := screen.Compile(screen.Config{
		LastNonblankRows: 4,
		Rules: []screen.Rule{
			{State: status.Working, All: []screen.Matcher{{Contains: "working"}}},
			{State: status.Idle, All: []screen.Matcher{{Contains: "ready"}}},
		},
	})
	require.NoError(t, err)
	return evaluator
}

func TestScreenRecaptureRecoversWithoutAnotherOutputHint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 1)
		screens.entries[panes[0].Ref.ID].plan.Evaluate = redrawEvaluator(t).Evaluate
		var captures atomic.Int64
		screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			results, err := successfulScreens(ctx, targets)
			if captures.Add(1) == 2 {
				results[0].Text = "partial redraw"
			}
			return results, err
		}
		_, _ = startLoop(t, inventoryLoop{
			screens: screens, nextRetry: noRetry,
			refresh: func(context.Context) (bool, error) { return false, nil },
		})
		synctest.Wait()
		time.Sleep(screenSettle)
		synctest.Wait()
		require.Equal(t, status.Working, paneScreenState(t, screens, panes[0].Ref))

		screens.Dirty(panes[0].Ref.ID)
		time.Sleep(screenBatchGap)
		synctest.Wait()

		require.Equal(t, status.Unknown, paneScreenState(t, screens, panes[0].Ref))
		require.Equal(t, int64(2), captures.Load())
		time.Sleep(screenBatchGap - time.Nanosecond)
		synctest.Wait()
		require.Equal(t, int64(2), captures.Load(), "recapture must respect the batch gap")
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		require.Equal(t, int64(3), captures.Load())
		require.Equal(t, status.Working, paneScreenState(t, screens, panes[0].Ref))
	})
}

func TestScreenRecaptureIsOneShotUntilPositiveEvidence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 1)
		screens.entries[panes[0].Ref.ID].plan.Evaluate = redrawEvaluator(t).Evaluate
		text := "partial redraw"
		screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			results, err := successfulScreens(ctx, targets)
			results[0].Text = text
			return results, err
		}
		time.Sleep(screenSettle)
		require.NoError(t, screens.runBatch(t.Context()))
		require.Equal(t, time.Now().Add(screenBatchGap), screens.nextCapture())

		time.Sleep(screenBatchGap)
		require.NoError(t, screens.runBatch(t.Context()))
		installScreenPlans(t, screens, panes, 2)

		require.Equal(t, status.Unknown, paneScreenState(t, screens, panes[0].Ref))
		require.Equal(t, time.Now().Add(screenRecovery), screens.nextCapture())
		for _, next := range []string{"partial redraw", "working\nready", "partial redraw"} {
			text = next
			time.Sleep(time.Until(screens.nextCapture()))
			require.NoError(t, screens.runBatch(t.Context()))
			require.Equal(t, time.Now().Add(screenRecovery), screens.nextCapture())
		}
		text = "ready"
		time.Sleep(time.Until(screens.nextCapture()))
		require.NoError(t, screens.runBatch(t.Context()))
		require.Equal(t, status.Idle, paneScreenState(t, screens, panes[0].Ref))
		text = "partial redraw"
		screens.Dirty(panes[0].Ref.ID)
		time.Sleep(screenBatchGap)
		require.NoError(t, screens.runBatch(t.Context()))
		require.Equal(t, time.Now().Add(screenBatchGap), screens.nextCapture())
	})
}

func TestConflictingScreenEvidenceDoesNotScheduleRecapture(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 1)
		screens.entries[panes[0].Ref.ID].plan.Evaluate = redrawEvaluator(t).Evaluate
		screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			results, err := successfulScreens(ctx, targets)
			results[0].Text = "working\nready"
			return results, err
		}
		time.Sleep(screenSettle)

		require.NoError(t, screens.runBatch(t.Context()))

		require.Equal(t, status.Unknown, paneScreenState(t, screens, panes[0].Ref))
		require.Equal(t, time.Now().Add(screenRecovery), screens.nextCapture())
	})
}

func TestFailedRecaptureBacksOffWithoutRearming(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 1)
		screens.entries[panes[0].Ref.ID].plan.Evaluate = redrawEvaluator(t).Evaluate
		noMatch := func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			results, err := successfulScreens(ctx, targets)
			results[0].Text = "partial redraw"
			return results, err
		}
		screens.capture = noMatch
		time.Sleep(screenSettle)
		require.NoError(t, screens.runBatch(t.Context()))
		screens.capture = unavailableScreen
		time.Sleep(screenBatchGap)

		err := screens.runBatch(t.Context())
		screens.Dirty(panes[0].Ref.ID)

		require.ErrorIs(t, err, observe.ErrChanged)
		require.GreaterOrEqual(t, time.Until(screens.nextCapture()), 400*time.Millisecond)
		screens.capture = noMatch
		time.Sleep(time.Until(screens.nextCapture()))
		require.NoError(t, screens.runBatch(t.Context()))
		require.Equal(t, time.Now().Add(screenRecovery), screens.nextCapture())
	})
}

func TestScreenScopeReplacementRearmsRecapture(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 1)
		evaluate := redrawEvaluator(t).Evaluate
		screens.entries[panes[0].Ref.ID].plan.Evaluate = evaluate
		screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			results, err := successfulScreens(ctx, targets)
			results[0].Text = "partial redraw"
			return results, err
		}
		time.Sleep(screenSettle)
		require.NoError(t, screens.runBatch(t.Context()))
		time.Sleep(screenBatchGap)
		require.NoError(t, screens.runBatch(t.Context()))
		require.Equal(t, time.Now().Add(screenRecovery), screens.nextCapture())

		panes[0].Scope.Connection = "replacement"
		installScreenPlans(t, screens, panes, 2)
		screens.entries[panes[0].Ref.ID].plan.Evaluate = evaluate
		time.Sleep(screenBatchGap)
		require.NoError(t, screens.runBatch(t.Context()))

		require.Equal(t, status.Unknown, paneScreenState(t, screens, panes[0].Ref))
		require.Equal(t, time.Now().Add(screenBatchGap), screens.nextCapture())
	})
}

func TestScreenRecaptureRetainsBatchBoundAndFairness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 300)
		evaluate := redrawEvaluator(t).Evaluate
		for _, entry := range screens.entries {
			entry.plan.Evaluate = evaluate
		}
		var sizes []int
		screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			sizes = append(sizes, len(targets))
			results, err := successfulScreens(ctx, targets)
			if len(sizes) == 1 {
				for i := range results {
					results[i].Text = "partial redraw"
				}
			}
			return results, err
		}
		time.Sleep(screenSettle)

		for range 4 {
			require.NoError(t, screens.runBatch(t.Context()))
			time.Sleep(screenBatchGap)
		}

		require.Equal(t, []int{128, 128, 128, 44}, sizes)
		for _, pane := range panes {
			require.Equal(t, status.Working, paneScreenState(t, screens, pane.Ref))
		}
	})
}
