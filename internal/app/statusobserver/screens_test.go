package statusobserver

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/screen"
	"github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func screenFixture(t *testing.T, count int) (*Screens, []service.Pane) {
	t.Helper()
	core, err := service.New("server")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, core.Close()) })
	panes := make([]service.Pane, count)
	for n := range panes {
		panes[n] = service.Pane{
			Ref:      status.PaneRef{Server: "server", ID: fmt.Sprintf("%%%04d", n), Generation: "pane-life"},
			Scope:    service.Scope{Root: "100:root", Foreground: "101:foreground", Connection: "connection"},
			Profile:  service.Profile{Source: service.Screen, Generation: "rules-1"},
			Geometry: service.Geometry{Columns: 80, Rows: 30},
		}
	}
	screens := newScreens(core, successfulScreens)
	screens.report = func(err error) { t.Errorf("unexpected capture error: %v", err) }
	installScreenPlans(t, screens, panes, 1)
	return screens, panes
}

func successfulScreens(_ context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
	screens := make([]observe.Screen, len(targets))
	for i, target := range targets {
		screens[i] = observe.Screen{Target: target, Text: "working", StartedAt: time.Now()}
	}
	return screens, nil
}

func installScreenPlans(t *testing.T, screens *Screens, panes []service.Pane, revision uint64) {
	t.Helper()
	err := screens.service.UpdateInventory(service.Inventory{
		Revision: revision, Panes: panes, ValidUntil: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	entries := make(map[string]*screenEntry, len(panes))
	for _, pane := range panes {
		target, ok := screens.service.Target(pane.Ref, service.Screen)
		require.True(t, ok)
		entries[pane.Ref.ID] = &screenEntry{
			target: target,
			plan:   ScreenPlan{Pane: pane.Ref, Evaluate: func(string) (status.State, error) { return status.Working, nil }},
		}
	}
	screens.mu.Lock()
	err = screens.replace(revision, entries)
	screens.mu.Unlock()
	require.NoError(t, err)
}

func paneScreenState(t *testing.T, screens *Screens, pane status.PaneRef) status.State {
	t.Helper()
	snapshot, err := screens.service.ReadStatus(t.Context())
	require.NoError(t, err)
	return snapshot.At(pane, time.Now())
}

func TestScreensCompiledRulesReplacePreviousEvidence(t *testing.T) {
	evaluator, err := screen.Compile(screen.Config{
		LastNonblankRows: 4,
		Rules: []screen.Rule{
			{State: status.Working, All: []screen.Matcher{{Contains: "working"}}},
			{State: status.Idle, All: []screen.Matcher{{Contains: "ready"}}},
		},
	})
	require.NoError(t, err)
	cases := []struct {
		name string
		text string
		want status.State
		err  error
	}{
		{"new activity", "ready", status.Idle, nil},
		{"no match", "different layout", status.Unknown, nil},
		{"conflict", "working\nready", status.Unknown, nil},
		{"invalid text", "\x1b[31mready", status.Unknown, screen.ErrInvalidScreen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				screens, panes := screenFixture(t, 1)
				screens.entries[panes[0].Ref.ID].plan.Evaluate = evaluator.Evaluate
				time.Sleep(screenSettle)
				require.NoError(t, screens.runBatch(t.Context()))
				require.Equal(t, status.Working, paneScreenState(t, screens, panes[0].Ref))
				screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
					captures, err := successfulScreens(ctx, targets)
					captures[0].Text = tc.text
					return captures, err
				}
				screens.Dirty(panes[0].Ref.ID)
				time.Sleep(screenBatchGap)

				err := screens.runBatch(t.Context())

				require.ErrorIs(t, err, tc.err)
				require.Equal(t, tc.want, paneScreenState(t, screens, panes[0].Ref))
			})
		})
	}
}

func TestScreensBoundDirtyDelayAndRecoverQuietPanes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 1)
		time.Sleep(screenSettle)
		require.NoError(t, screens.runBatch(t.Context()))
		started := time.Now()

		for range 12 {
			screens.Dirty(panes[0].Ref.ID)
			time.Sleep(20 * time.Millisecond)
		}
		due := screens.nextCapture()
		time.Sleep(time.Until(due))
		err := screens.runBatch(t.Context())

		require.NoError(t, err)
		require.Equal(t, started.Add(screenMaxDirtyAge), due)
		require.Equal(t, status.Working, paneScreenState(t, screens, panes[0].Ref))
		require.Equal(t, time.Now().Add(screenRecovery), screens.nextCapture())
		time.Sleep(screenFreshness)
		require.Equal(t, status.Unknown, paneScreenState(t, screens, panes[0].Ref))
	})
}

func TestScreensUseProfileRecoveryAndFreshness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 1)
		entry := screens.entries[panes[0].Ref.ID]
		entry.plan.PollInterval = 30 * time.Second
		time.Sleep(screenSettle)
		started := time.Now()

		err := screens.runBatch(t.Context())

		require.NoError(t, err)
		require.Equal(t, started.Add(30*time.Second), screens.nextCapture())
		time.Sleep(30 * time.Second)
		require.Equal(t, status.Working, paneScreenState(t, screens, panes[0].Ref))
		time.Sleep(screenGrace)
		require.Equal(t, status.Unknown, paneScreenState(t, screens, panes[0].Ref))
	})
}

func TestSelectionRejectsInvalidRecoveryIntervals(t *testing.T) {
	for _, interval := range []time.Duration{-time.Second, time.Millisecond, time.Minute + time.Second} {
		selection := Selection{
			Profile:      service.Profile{Source: service.Screen, Generation: "fixture"},
			Evaluate:     func(string) (status.State, error) { return status.Idle, nil },
			PollInterval: interval,
		}

		err := validateSelection(selection)

		require.Error(t, err)
	}
}

func TestScreensServeMoreThanOneBatchWithoutHotPaneStarvation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 300)
		var sizes []int
		screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			sizes = append(sizes, len(targets))
			return successfulScreens(ctx, targets)
		}
		time.Sleep(screenSettle)

		for range 3 {
			require.NoError(t, screens.runBatch(t.Context()))
			for _, pane := range panes[:screenBatchSize] {
				screens.Dirty(pane.Ref.ID)
			}
			time.Sleep(screenBatchGap)
		}

		require.Equal(t, []int{128, 128, 128}, sizes)
		for _, pane := range panes {
			require.Equal(t, status.Working, paneScreenState(t, screens, pane.Ref))
		}
	})
}

func TestScreensRejectResultAcrossProfileChangeAndBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 1)
		started, release := make(chan struct{}), make(chan struct{})
		var evaluations atomic.Int64
		screens.entries[panes[0].Ref.ID].plan.Evaluate = func(string) (status.State, error) {
			evaluations.Add(1)
			return status.Working, nil
		}
		screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			close(started)
			select {
			case <-release:
				return successfulScreens(ctx, targets)
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		time.Sleep(screenSettle)
		done := make(chan error, 1)
		go func() { done <- screens.runBatch(t.Context()) }()
		await(t, started)

		panes[0].Profile.Generation = "rules-2"
		installScreenPlans(t, screens, panes, 2)
		panes[0].Profile.Generation = "rules-1"
		installScreenPlans(t, screens, panes, 3)
		close(release)
		require.NoError(t, awaitScreenBatch(t, done))

		require.Zero(t, evaluations.Load())
		require.Equal(t, status.Unknown, paneScreenState(t, screens, panes[0].Ref))
	})
}

func awaitScreenBatch(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("screen acquisition did not finish")
		return nil
	}
}

func TestScreensRetainDirtyHintDuringCapture(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 1)
		started, release := make(chan struct{}), make(chan struct{})
		screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			close(started)
			<-release
			return successfulScreens(ctx, targets)
		}
		time.Sleep(screenSettle)
		done := make(chan error, 1)
		go func() { done <- screens.runBatch(t.Context()) }()
		await(t, started)

		screens.Dirty(panes[0].Ref.ID)
		close(release)
		require.NoError(t, awaitScreenBatch(t, done))
		due := screens.nextCapture()

		require.Equal(t, time.Now().Add(screenBatchGap), due)
		require.Equal(t, status.Working, paneScreenState(t, screens, panes[0].Ref))
	})
}

func TestScreenFailuresBecomeUnknownAndBackOff(t *testing.T) {
	cases := []struct {
		name     string
		capture  func(context.Context, []observe.CaptureTarget) ([]observe.Screen, error)
		evaluate func(string) (status.State, error)
	}{
		{"query failure", func(context.Context, []observe.CaptureTarget) ([]observe.Screen, error) {
			return nil, errors.New("query failed")
		}, nil},
		{"incomplete batch", func(context.Context, []observe.CaptureTarget) ([]observe.Screen, error) { return nil, nil }, nil},
		{"unavailable pane", unavailableScreen, nil},
		{"expired capture", expiredScreen, nil},
		{"evaluation failure", successfulScreens, func(string) (status.State, error) { return "", errors.New("evaluation failed") }},
		{"invalid state", successfulScreens, func(string) (status.State, error) { return status.Withdrawn, nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				screens, panes := screenFixture(t, 1)
				time.Sleep(screenSettle)
				require.NoError(t, screens.runBatch(t.Context()))
				screens.capture = tc.capture
				if tc.evaluate != nil {
					screens.entries[panes[0].Ref.ID].plan.Evaluate = tc.evaluate
				}
				screens.Dirty(panes[0].Ref.ID)
				time.Sleep(screenBatchGap)

				err := screens.runBatch(t.Context())
				screens.Dirty(panes[0].Ref.ID)

				require.Error(t, err)
				require.Equal(t, status.Unknown, paneScreenState(t, screens, panes[0].Ref))
				require.GreaterOrEqual(t, time.Until(screens.nextCapture()), 400*time.Millisecond)
			})
		})
	}
}

func unavailableScreen(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
	screens, err := successfulScreens(ctx, targets)
	screens[0].Err = observe.ErrChanged
	return screens, err
}

func expiredScreen(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
	screens, err := successfulScreens(ctx, targets)
	screens[0].StartedAt = time.Now().Add(-screenFreshness)
	return screens, err
}

func TestScreensKeepValidPeerWhenOneCaptureChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 2)
		screens.capture = unavailableScreen
		time.Sleep(screenSettle)

		err := screens.runBatch(t.Context())

		require.ErrorIs(t, err, observe.ErrChanged)
		require.Equal(t, status.Unknown, paneScreenState(t, screens, panes[0].Ref))
		require.Equal(t, status.Working, paneScreenState(t, screens, panes[1].Ref))
	})
}

func TestScreensCancelWithoutApplyingPartialText(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, panes := screenFixture(t, 1)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			cancel()
			return successfulScreens(ctx, targets)
		}
		time.Sleep(screenSettle)

		err := screens.runBatch(ctx)

		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, status.Unknown, paneScreenState(t, screens, panes[0].Ref))
	})
}

func TestSharedWatchPrioritizesInventoryAndRunsOnlyOneAcquisition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		screens, _ := screenFixture(t, 1)
		release := make(chan struct{})
		var inventories, captures atomic.Int64
		screens.capture = func(ctx context.Context, targets []observe.CaptureTarget) ([]observe.Screen, error) {
			captures.Add(1)
			return successfulScreens(ctx, targets)
		}
		_, _ = startLoop(t, inventoryLoop{
			screens: screens, nextRetry: noRetry,
			refresh: func(ctx context.Context) (bool, error) {
				if inventories.Add(1) == 1 {
					select {
					case <-release:
					case <-ctx.Done():
						return false, ctx.Err()
					}
				}
				return false, nil
			},
		})
		synctest.Wait()

		time.Sleep(recoveryInterval)
		synctest.Wait()
		require.Zero(t, captures.Load())
		close(release)
		synctest.Wait()

		require.Equal(t, int64(1), inventories.Load())
		require.Equal(t, int64(1), captures.Load())
	})
}
