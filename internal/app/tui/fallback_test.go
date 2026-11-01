package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestPickerUsesBoundedFallbackWhenStatusIsUnavailable(t *testing.T) {
	statusFailure := errors.New("status identity unavailable")
	server := observe.Server{PID: 123, Birth: "fixture", Started: 1}
	for _, test := range []struct {
		name         string
		state        backend.StateResult
		statusErr    error
		wantItems    int
		wantFallback int
	}{
		{"combined success", liveState(), nil, 1, 0},
		{"status unavailable with rows", liveState(), statusFailure, 1, 1},
		{"status unavailable with empty inventory", backend.StateResult{}, statusFailure, 0, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := &projectionStatusBackend{state: test.state, server: server, statusErr: test.statusErr}
			adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)

			snapshot, err := adapter.Load(t.Context(), ui.ViewFlat)
			reader, statusErr := adapter.OpenStatus(t.Context())

			require.NoError(t, err)
			require.Len(t, snapshot.Items, test.wantItems)
			require.Equal(t, test.wantFallback, b.limitedCalls)
			require.Equal(t, 1+test.wantFallback, b.queryCalls)
			if test.statusErr != nil {
				require.Nil(t, reader, "failed combined query must not supply a status server")
				require.ErrorIs(t, statusErr, statusFailure)
			} else {
				require.NoError(t, statusErr)
				require.NotNil(t, reader)
			}
		})
	}
}

func TestPickerDoesNotRetryTerminalQueryFailures(t *testing.T) {
	interruptErr := exec.CommandContext(t.Context(), "sh", "-c", "kill -INT $$").Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, interruptErr, &exitErr)
	diagnostic := errors.New("query diagnostic")

	for _, test := range []struct {
		name string
		err  error
	}{
		{"output overflow", backend.ErrOutputLimitExceeded},
		{"cancellation", context.Canceled},
		{"child deadline with active caller", context.DeadlineExceeded},
		{"owned process interruption", &backend.CanceledProcessError{Cause: exitErr}},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := &projectionStatusBackend{
				state:     liveState(),
				statusErr: fmt.Errorf("combined query: %w", errors.Join(test.err, diagnostic)),
			}
			adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)

			_, err := adapter.Load(t.Context(), ui.ViewFlat)

			require.ErrorIs(t, err, test.err)
			require.ErrorIs(t, err, diagnostic)
			require.Equal(t, 1, b.queryCalls)
			require.Zero(t, b.limitedCalls)
			require.NoError(t, t.Context().Err())
		})
	}
}

func TestPickerDoesNotRetryAfterCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	statusFailure := errors.New("status identity unavailable")
	b := &projectionStatusBackend{
		state:      liveState(),
		statusErr:  statusFailure,
		afterQuery: cancel,
	}
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)

	_, err := adapter.Load(ctx, ui.ViewFlat)

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, statusFailure)
	require.Equal(t, 1, b.queryCalls)
	require.Zero(t, b.limitedCalls)
}

func TestPickerDoesNotUseUnboundedFallbackWhenCapabilityIsMissing(t *testing.T) {
	statusFailure := errors.New("status identity unavailable")
	b := &projectionStatusBackend{state: liveState(), statusErr: statusFailure}
	// Expose the original capabilities, but not the bounded fallback method.
	provider := struct {
		Backend
		statusStateProvider
	}{b, b}
	adapter := newLiveAdapter(func() (Backend, error) { return provider, nil }, nil, nil)

	_, err := adapter.Load(t.Context(), ui.ViewFlat)

	require.ErrorIs(t, err, statusFailure)
	require.Equal(t, 1, b.queryCalls)
	require.Zero(t, b.limitedCalls)
}

func TestPickerRetainsBothQueryFailures(t *testing.T) {
	statusFailure := errors.New("status identity unavailable")
	fallbackFailure := errors.New("inventory unavailable")
	b := &projectionStatusBackend{
		stateErr:  fallbackFailure,
		statusErr: fmt.Errorf("combined query: %w", statusFailure),
	}
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)

	_, err := adapter.Load(t.Context(), ui.ViewFlat)

	require.ErrorIs(t, err, statusFailure)
	require.ErrorIs(t, err, fallbackFailure)
	require.Equal(t, 1, b.limitedCalls)
	require.Equal(t, 2, b.queryCalls)
}
