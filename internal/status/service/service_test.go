package service_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	statusservice "github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func fixturePane(source statusservice.Source) statusservice.Pane {
	pane := statusservice.Pane{
		Ref: status.PaneRef{Server: "server-birth", ID: "%1", Generation: "pane-birth"},
		Scope: statusservice.Scope{
			Root:       "shell-birth",
			Foreground: "application-birth",
			Connection: "connection-1",
			Command:    "invocation-1",
		},
		Geometry: statusservice.Geometry{Columns: 100, Rows: 30},
	}
	if source != "" {
		pane.Profile = statusservice.Profile{Source: source, Generation: "profile-1"}
	}

	return pane
}

func fixtureService(t testing.TB, panes ...statusservice.Pane) *statusservice.Service {
	t.Helper()

	service, err := statusservice.New("server-birth")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, service.Close())
	})
	require.NoError(t, service.UpdateInventory(inventory(1, panes...)))
	return service
}

func inventory(revision uint64, panes ...statusservice.Pane) statusservice.Inventory {
	return statusservice.Inventory{
		Revision:   revision,
		ValidUntil: time.Now().Add(time.Minute),
		Panes:      panes,
	}
}

func targetFor(t testing.TB, service *statusservice.Service, pane statusservice.Pane, source statusservice.Source) statusservice.Target {
	t.Helper()

	target, ok := service.Target(pane.Ref, source)
	require.True(t, ok, "expected a target for the selected source")
	return target
}

func TestSelectedSourceControlsAvailability(t *testing.T) {
	cases := []struct {
		name    string
		profile statusservice.Source
		report  status.State
		want    status.State
	}{
		{name: "no evidence", want: status.Unreported},
		{name: "generic protocol", report: status.Working, want: status.Working},
		{name: "explicit clear", report: status.Withdrawn, want: status.Withdrawn},
		{name: "missing screen evidence", profile: statusservice.Screen, want: status.Unknown},
		{name: "screen evidence", profile: statusservice.Screen, report: status.Blocked, want: status.Blocked},
		{name: "screen failure", profile: statusservice.Screen, report: status.Unknown, want: status.Unknown},
		{name: "missing profiled protocol", profile: statusservice.OSC7501, want: status.Unknown},
		{name: "profiled protocol", profile: statusservice.OSC7501, report: status.Idle, want: status.Idle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pane := fixturePane(tc.profile)
			service := fixtureService(t, pane)
			source := statusservice.OSC7501
			if tc.profile == statusservice.Screen {
				source = statusservice.Screen
			}
			target := targetFor(t, service, pane, source)
			value := status.Value{State: tc.report}
			if source == statusservice.Screen && tc.report == status.Blocked {
				value.ValidUntil = time.Now().Add(time.Second)
			}

			var applyErr error
			if tc.report != status.Unreported {
				applyErr = service.Apply(target, 1, value)
			}
			snapshot, readErr := service.ReadStatus(t.Context())

			require.NoError(t, applyErr)
			require.NoError(t, readErr)
			require.Equal(t, tc.want, snapshot.At(pane.Ref, time.Now()))
		})
	}
}

func TestScreenClaimDoesNotFallBackToProtocol(t *testing.T) {
	pane := fixturePane("")
	service := fixtureService(t, pane)
	protocol := targetFor(t, service, pane, statusservice.OSC7501)
	require.NoError(t, service.Apply(protocol, 1, status.Value{State: status.Working}))
	pane.Profile = statusservice.Profile{Source: statusservice.Screen, Generation: "screen-profile-1"}
	require.NoError(t, service.UpdateInventory(inventory(2, pane)))

	lateErr := service.Apply(protocol, 2, status.Value{State: status.Idle})
	_, available := service.Target(pane.Ref, statusservice.OSC7501)
	snapshot, readErr := service.ReadStatus(t.Context())

	require.ErrorIs(t, lateErr, statusservice.ErrStale)
	require.False(t, available)
	require.NoError(t, readErr)
	require.Equal(t, status.Unknown, snapshot.At(pane.Ref, time.Now()))
}

func TestChangedAndRestoredScopeRejectsOldCapture(t *testing.T) {
	cases := []struct {
		name   string
		change func(*statusservice.Pane)
	}{
		{"pane incarnation", func(p *statusservice.Pane) { p.Ref.Generation = "replacement" }},
		{"root", func(p *statusservice.Pane) { p.Scope.Root = "replacement" }},
		{"foreground", func(p *statusservice.Pane) { p.Scope.Foreground = "replacement" }},
		{"connection", func(p *statusservice.Pane) { p.Scope.Connection = "replacement" }},
		{"command boundary", func(p *statusservice.Pane) { p.Scope.Command = "replacement" }},
		{"profile", func(p *statusservice.Pane) { p.Profile.Generation = "replacement" }},
		{"geometry", func(p *statusservice.Pane) { p.Geometry.Columns++ }},
		{"missing identity", func(p *statusservice.Pane) { p.Scope.Foreground = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pane := fixturePane(statusservice.Screen)
			service := fixtureService(t, pane)
			old := targetFor(t, service, pane, statusservice.Screen)
			working := status.Value{State: status.Working, ValidUntil: time.Now().Add(time.Minute)}
			require.NoError(t, service.Apply(old, 1, working))
			changed := pane
			tc.change(&changed)
			require.NoError(t, service.UpdateInventory(inventory(2, changed)))
			require.NoError(t, service.UpdateInventory(inventory(3, pane)))
			fresh := targetFor(t, service, pane, statusservice.Screen)

			lateErr := service.Apply(old, 2, working)
			invalidated, invalidatedErr := service.ReadStatus(t.Context())
			freshErr := service.Apply(fresh, 1, working)
			recovered, recoveredErr := service.ReadStatus(t.Context())

			require.ErrorIs(t, lateErr, statusservice.ErrStale)
			require.NoError(t, invalidatedErr)
			require.Equal(t, status.Unknown, invalidated.At(pane.Ref, time.Now()))
			require.NoError(t, freshErr)
			require.NoError(t, recoveredErr)
			require.Equal(t, status.Working, recovered.At(pane.Ref, time.Now()))
		})
	}
}

func TestInventoryRefreshKeepsProtocolButDoesNotRefreshCapture(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		protocolPane := fixturePane("")
		screenPane := fixturePane(statusservice.Screen)
		screenPane.Ref.ID = "%2"
		service := fixtureService(t, protocolPane, screenPane)
		protocol := targetFor(t, service, protocolPane, statusservice.OSC7501)
		screen := targetFor(t, service, screenPane, statusservice.Screen)
		require.NoError(t, service.Apply(protocol, 1, status.Value{State: status.Working}))
		require.NoError(t, service.Apply(screen, 1, status.Value{
			State: status.Blocked, ValidUntil: time.Now().Add(3 * time.Second),
		}))
		original, err := service.ReadStatus(t.Context())
		require.NoError(t, err)
		time.Sleep(2 * time.Second)
		protocolPane.Geometry.Columns++

		refreshErr := service.UpdateInventory(inventory(2, protocolPane, screenPane))
		time.Sleep(2 * time.Second)
		snapshot, readErr := service.ReadStatus(t.Context())

		require.NoError(t, refreshErr)
		require.NoError(t, readErr)
		require.Equal(t, status.Working, snapshot.At(protocolPane.Ref, time.Now()))
		require.Equal(t, status.Unknown, snapshot.At(screenPane.Ref, time.Now()))
		require.Equal(t, original.Lookup(screenPane.Ref).ValidUntil, snapshot.Lookup(screenPane.Ref).ValidUntil)
		require.True(t, snapshot.Lookup(protocolPane.Ref).ValidUntil.After(original.Lookup(protocolPane.Ref).ValidUntil))
	})
}

func TestInventoryGapRequiresFreshEvidence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pane := fixturePane("")
		service := fixtureService(t, pane)
		old := targetFor(t, service, pane, statusservice.OSC7501)
		require.NoError(t, service.Apply(old, 1, status.Value{State: status.Working}))
		time.Sleep(time.Minute)

		_, duringGap := service.Target(pane.Ref, statusservice.OSC7501)
		refreshErr := service.UpdateInventory(inventory(2, pane))
		lateErr := service.Apply(old, 2, status.Value{State: status.Idle})
		snapshot, readErr := service.ReadStatus(t.Context())

		require.False(t, duringGap)
		require.NoError(t, refreshErr)
		require.ErrorIs(t, lateErr, statusservice.ErrStale)
		require.NoError(t, readErr)
		require.Equal(t, status.Unknown, snapshot.At(pane.Ref, time.Now()))
	})
}

func TestInvalidationPreservesAbsentWithdrawnAndUnknownDistinctions(t *testing.T) {
	absent := fixturePane("")
	withdrawn, active := absent, absent
	withdrawn.Ref.ID = "%2"
	active.Ref.ID = "%3"
	service := fixtureService(t, absent, withdrawn, active)
	clearTarget := targetFor(t, service, withdrawn, statusservice.OSC7501)
	activeTarget := targetFor(t, service, active, statusservice.OSC7501)
	require.NoError(t, service.Apply(clearTarget, 1, status.Value{State: status.Withdrawn}))
	require.NoError(t, service.Apply(activeTarget, 1, status.Value{State: status.Working}))

	invalidateErr := service.Invalidate(3)
	lateInventoryErr := service.UpdateInventory(inventory(2, absent, withdrawn, active))
	refreshErr := service.UpdateInventory(inventory(4, absent, withdrawn, active))
	lateEvidenceErr := service.Apply(activeTarget, 2, status.Value{State: status.Idle})
	snapshot, readErr := service.ReadStatus(t.Context())

	require.NoError(t, invalidateErr)
	require.ErrorIs(t, lateInventoryErr, statusservice.ErrStale)
	require.NoError(t, refreshErr)
	require.ErrorIs(t, lateEvidenceErr, statusservice.ErrStale)
	require.NoError(t, readErr)
	require.Equal(t, status.Unreported, snapshot.At(absent.Ref, time.Now()))
	require.Equal(t, status.Withdrawn, snapshot.At(withdrawn.Ref, time.Now()))
	require.Equal(t, status.Unknown, snapshot.At(active.Ref, time.Now()))
}

func TestOutOfOrderCaptureCannotUndoFailure(t *testing.T) {
	pane := fixturePane(statusservice.Screen)
	service := fixtureService(t, pane)
	target := targetFor(t, service, pane, statusservice.Screen)
	require.NoError(t, service.Apply(target, 2, status.Value{State: status.Unknown}))

	lateErr := service.Apply(target, 1, status.Value{State: status.Working, ValidUntil: time.Now().Add(time.Second)})
	expiredErr := service.Apply(target, 3, status.Value{State: status.Idle, ValidUntil: time.Now().Add(-time.Second)})
	snapshot, readErr := service.ReadStatus(t.Context())

	require.ErrorIs(t, lateErr, statusservice.ErrStale)
	require.ErrorIs(t, expiredErr, statusservice.ErrStale)
	require.NoError(t, readErr)
	require.Equal(t, status.Unknown, snapshot.At(pane.Ref, time.Now()))
}

func TestInvalidEvidenceCannotOverwriteCurrentStatus(t *testing.T) {
	cases := []struct {
		name   string
		source statusservice.Source
		value  status.Value
	}{
		{"absence is not clear", statusservice.OSC7501, status.Value{}},
		{"protocol has no heartbeat", statusservice.OSC7501, status.Value{State: status.Idle, ValidUntil: time.Now().Add(time.Minute)}},
		{"screen needs freshness", statusservice.Screen, status.Value{State: status.Working}},
		{"screen cannot clear", statusservice.Screen, status.Value{State: status.Withdrawn}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pane := fixturePane(tc.source)
			service := fixtureService(t, pane)
			target := targetFor(t, service, pane, tc.source)
			valid := status.Value{State: status.Blocked}
			if tc.source == statusservice.Screen {
				valid.ValidUntil = time.Now().Add(time.Minute)
			}
			require.NoError(t, service.Apply(target, 1, valid))

			applyErr := service.Apply(target, 2, tc.value)
			snapshot, readErr := service.ReadStatus(t.Context())

			require.Error(t, applyErr)
			require.NoError(t, readErr)
			require.Equal(t, status.Blocked, snapshot.At(pane.Ref, time.Now()))
		})
	}
}

func TestTargetCannotCrossServiceInstances(t *testing.T) {
	pane := fixturePane("")
	previous := fixtureService(t, pane)
	target := targetFor(t, previous, pane, statusservice.OSC7501)
	current := fixtureService(t, pane)

	err := current.Apply(target, 1, status.Value{State: status.Working})
	snapshot, readErr := current.ReadStatus(t.Context())

	require.ErrorIs(t, err, statusservice.ErrStale)
	require.NoError(t, readErr)
	require.Zero(t, snapshot.Len())
}

func TestInvalidInventoryIsRejectedAtomically(t *testing.T) {
	cases := []struct {
		name   string
		change func(*statusservice.Inventory)
	}{
		{"another server", func(i *statusservice.Inventory) { i.Panes[0].Ref.Server = "other-server" }},
		{"duplicate incarnation", func(i *statusservice.Inventory) {
			duplicate := i.Panes[0]
			duplicate.Ref.Generation = "another-birth"
			i.Panes = append(i.Panes, duplicate)
		}},
		{"oversized identity", func(i *statusservice.Inventory) { i.Panes[0].Scope.Root = strings.Repeat("x", 257) }},
		{"unknown source", func(i *statusservice.Inventory) { i.Panes[0].Profile.Source = "other" }},
		{"pane cap", func(i *statusservice.Inventory) { i.Panes = make([]statusservice.Pane, status.MaxPanes+1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pane := fixturePane("")
			service := fixtureService(t, pane)
			target := targetFor(t, service, pane, statusservice.OSC7501)
			require.NoError(t, service.Apply(target, 1, status.Value{State: status.Working}))
			invalid := inventory(2, pane)
			tc.change(&invalid)

			updateErr := service.UpdateInventory(invalid)
			snapshot, readErr := service.ReadStatus(t.Context())

			require.Error(t, updateErr)
			require.NoError(t, readErr)
			require.Equal(t, status.Working, snapshot.At(pane.Ref, time.Now()))
		})
	}
}

func TestRetirementAndClosePreservePreviouslyReturnedSnapshots(t *testing.T) {
	pane := fixturePane("")
	service := fixtureService(t, pane)
	target := targetFor(t, service, pane, statusservice.OSC7501)
	require.NoError(t, service.Apply(target, 1, status.Value{State: status.Working}))
	original, err := service.ReadStatus(t.Context())
	require.NoError(t, err)

	retireErr := service.UpdateInventory(inventory(2))
	retired, retiredErr := service.ReadStatus(t.Context())
	lateErr := service.Apply(target, 2, status.Value{State: status.Idle})
	closeErr := service.Close()
	_, closedErr := service.ReadStatus(t.Context())
	updateErr := service.UpdateInventory(inventory(3, pane))

	require.NoError(t, retireErr)
	require.NoError(t, retiredErr)
	require.Zero(t, retired.Len())
	require.ErrorIs(t, lateErr, statusservice.ErrStale)
	require.NoError(t, closeErr)
	require.ErrorIs(t, closedErr, statusservice.ErrClosed)
	require.ErrorIs(t, updateErr, statusservice.ErrClosed)
	require.Equal(t, status.Working, original.At(pane.Ref, time.Now()))
}

func TestReadHonorsCancellation(t *testing.T) {
	service := fixtureService(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := service.ReadStatus(ctx)

	require.ErrorIs(t, err, context.Canceled)
}

func TestConcurrentReadersAndEvidenceUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pane := fixturePane("")
		service := fixtureService(t, pane)
		target := targetFor(t, service, pane, statusservice.OSC7501)
		require.NoError(t, service.Apply(target, 1, status.Value{State: status.Working}))
		var workers sync.WaitGroup
		errs := make(chan error, 4)

		workers.Go(func() {
			for sequence := uint64(2); sequence < 130; sequence++ {
				state := status.Working
				if sequence%2 == 0 {
					state = status.Blocked
				}
				if err := service.Apply(target, sequence, status.Value{State: state}); err != nil {
					errs <- err
					return
				}
			}
		})
		for range 3 {
			workers.Go(func() {
				errs <- readWhileUpdating(service, pane.Ref)
			})
		}
		workers.Wait()
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}
	})
}

func readWhileUpdating(service *statusservice.Service, pane status.PaneRef) error {
	for range 128 {
		snapshot, err := service.ReadStatus(context.Background())
		if err != nil {
			return err
		}

		state := snapshot.At(pane, time.Now())
		if state != status.Working && state != status.Blocked {
			return fmt.Errorf("unexpected concurrent state: %s", state)
		}
	}

	return nil
}
