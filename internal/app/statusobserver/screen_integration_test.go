//go:build integration

package statusobserver

import (
	"context"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/screen"
	"github.com/MuhametSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func nativeScreenPane(server string, facts observe.Pane) service.Pane {
	return service.Pane{
		Ref:      status.PaneRef{Server: server, ID: facts.ID, Generation: "fixture-pane-life"},
		Scope:    service.Scope{Root: facts.Root.Identity(), Foreground: facts.Foreground.Identity(), Connection: "fixture-stream"},
		Profile:  service.Profile{Source: service.Screen, Generation: "fixture-rules"},
		Geometry: service.Geometry{Columns: facts.Columns, Rows: facts.Rows},
	}
}

func TestNativeScheduledScreenReachesServiceAndRevokesOnStop(t *testing.T) {
	runner, server, sampler, pane, _ := nativeStreamsFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err := runner.RunLimited(ctx, 4096, "send-keys", "-t", pane, "-l", "--", "NATIVE-SCREEN-READY\n")
	require.NoError(t, err)
	evaluator, err := screen.Compile(screen.Config{
		LastNonblankRows: 4,
		Rules: []screen.Rule{{
			State: status.Idle, All: []screen.Matcher{{Contains: "NATIVE-SCREEN-READY"}},
		}},
	})
	require.NoError(t, err)
	core, err := service.New(server.Identity())
	require.NoError(t, err)
	defer func() { require.NoError(t, core.Close()) }()
	reported := make(chan error, 1)
	screens, err := NewScreens(sampler, core, func(err error) {
		select {
		case reported <- err:
		default:
		}
	})
	require.NoError(t, err)
	streams, err := NewStreams(ctx, server, &recorder{})
	require.NoError(t, err)
	defer func() { require.NoError(t, streams.Close()) }()
	done := make(chan error, 1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		done <- Watch(ctx, sampler, streams, screens, func(inventory observe.Inventory, err error) error {
			if err != nil {
				return err
			}
			return publishNativeScreen(core, screens, inventory, pane, evaluator)
		})
	}()
	defer func() {
		cancel()
		await(t, joined)
	}()
	ref := status.PaneRef{Server: server.Identity(), ID: pane, Generation: "fixture-pane-life"}

	require.Eventually(t, func() bool {
		snapshot, readErr := core.ReadStatus(ctx)
		return readErr == nil && snapshot.At(ref, time.Now()) == status.Idle
	}, 3*time.Second, 10*time.Millisecond)
	cancel()
	err = awaitScreenBatch(t, done)
	state := paneScreenState(t, screens, ref)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, status.Unknown, state)
	select {
	case err := <-reported:
		t.Fatalf("unexpected screen diagnostic: %v", err)
	default:
	}
}

func publishNativeScreen(core *service.Service, screens *Screens, inventory observe.Inventory, id string, evaluator *screen.Evaluator) error {
	facts, _ := inventory.Lookup(id)
	pane := nativeScreenPane(inventory.Server(), facts)
	if err := core.UpdateInventory(service.Inventory{
		Revision: inventory.Revision(), Panes: []service.Pane{pane}, ValidUntil: inventory.StartedAt().Add(10 * time.Second),
	}); err != nil {
		return err
	}
	return screens.Update(inventory, []ScreenPlan{{
		Pane:     pane.Ref,
		Evaluate: evaluator.Evaluate,
	}})
}

func TestNativeScreenPlanRejectsMismatchedServiceScope(t *testing.T) {
	_, server, sampler, id, _ := nativeStreamsFixture(t)
	inventory, err := sampler.Inventory(t.Context(), 1)
	require.NoError(t, err)
	facts, found := inventory.Lookup(id)
	require.True(t, found)
	core, err := service.New(server.Identity())
	require.NoError(t, err)
	defer func() { require.NoError(t, core.Close()) }()
	pane := nativeScreenPane(server.Identity(), facts)
	pane.Scope.Root = "obsolete-root"
	require.NoError(t, core.UpdateInventory(service.Inventory{
		Revision: 1, Panes: []service.Pane{pane}, ValidUntil: time.Now().Add(time.Minute),
	}))
	screens, err := NewScreens(sampler, core, func(err error) { t.Errorf("unexpected asynchronous error: %v", err) })
	require.NoError(t, err)

	err = screens.Update(inventory, []ScreenPlan{{Pane: pane.Ref, Evaluate: func(string) (screen.Result, error) {
		return screen.Result{State: status.Idle, Reason: screen.Matched}, nil
	}}})

	require.ErrorIs(t, err, observe.ErrChanged)
	require.True(t, screens.nextCapture().IsZero())
	require.Equal(t, status.Unknown, paneScreenState(t, screens, pane.Ref))
}
