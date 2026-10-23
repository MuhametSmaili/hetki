package statusobserver

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/MuhametSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func delayedScreenRenewalSource(t *testing.T) (*Source, status.PaneRef) {
	t.Helper()
	screens, panes := screenFixture(t, 1)
	source := &Source{
		server:    "server",
		core:      screens.service,
		locations: []observe.Location{{Session: "$1", Window: "@1", Pane: panes[0].Ref.ID}},
	}
	revision := uint64(1)
	_, _ = startLoop(t, inventoryLoop{
		changes:   make(chan struct{}, 1),
		nextRetry: noRetry,
		screens:   screens,
		refresh: func(ctx context.Context) (bool, error) {
			started := time.Now()
			revision++
			// Delay the inventory pass after the same-state recovery capture.
			if revision == 4 {
				select {
				case <-time.After(1500 * time.Millisecond):
				case <-ctx.Done():
					return false, ctx.Err()
				}
			}

			source.mu.Lock()
			defer source.mu.Unlock()
			source.validatedUntil = started.Add(sourceValidation)
			err := source.core.UpdateInventory(service.Inventory{
				Revision:   revision,
				ValidUntil: source.validatedUntil,
				Panes:      panes,
			})
			return false, err
		},
	})
	return source, panes[0].Ref
}

func TestScreenRenewalsReachBothReadersBeforeDelayedInventory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source, pane := delayedScreenRenewalSource(t)
		synctest.Wait()
		time.Sleep(recoveryInterval)
		synctest.Wait()
		var before, renewed [2]daemon.View
		var changes [2]<-chan struct{}
		for i := range before {
			changes[i] = source.Changes()
			view, err := source.ReadView(t.Context())
			require.NoError(t, err)
			view.Values = view.Values.Leased(view.Lease)
			before[i] = view
			require.Equal(t, status.Working, view.Values.At(pane, time.Now()))
		}

		time.Sleep(screenSettle)
		synctest.Wait()

		for i := range renewed {
			select {
			case <-changes[i]:
			default:
				t.Fatal("screen extension did not wake a reader before the next inventory")
			}
			view, err := source.ReadView(t.Context())
			require.NoError(t, err)
			view.Values = view.Values.Leased(view.Lease)
			renewed[i] = view
			deadline := before[i].Values.Lookup(pane).ValidUntil
			require.True(t, time.Now().Before(deadline))
			require.True(t, view.Values.Lookup(pane).ValidUntil.After(deadline))
			changes[i] = source.Changes()
		}

		time.Sleep(time.Until(before[0].Values.Lookup(pane).ValidUntil))
		synctest.Wait()

		for i := range renewed {
			require.Equal(t, status.Unknown, before[i].Values.At(pane, time.Now()))
			require.Equal(t, status.Working, renewed[i].Values.At(pane, time.Now()))
			select {
			case <-changes[i]:
				t.Fatal("delayed inventory unexpectedly published before cached expiry")
			default:
			}
		}
	})
}
