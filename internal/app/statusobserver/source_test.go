package statusobserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func sourceFacts() observe.Pane {
	return observe.Pane{
		ID:         "%1",
		RootPID:    100,
		TTY:        "/dev/fixture",
		Command:    "shell",
		Columns:    80,
		Rows:       30,
		Root:       observe.Process{PID: 100, Birth: "root"},
		Foreground: observe.Process{PID: 101, Birth: "foreground"},
	}
}

func sourceScope(facts observe.Pane) Scope {
	return Scope{
		Pane:       facts.ID,
		Root:       facts.Root,
		Foreground: facts.Foreground,
		Connection: "first",
		Generation: 1,
	}
}

func TestSourcePaneIncarnationSeparatesRootAndStreamLifetimes(t *testing.T) {
	cases := []struct {
		name     string
		change   func(*observe.Pane)
		replaced bool
	}{
		{"unchanged", func(*observe.Pane) {}, false},
		{"resize", func(p *observe.Pane) { p.Columns++ }, false},
		{"foreground handoff", func(p *observe.Pane) { p.Foreground.Birth = "other" }, false},
		{
			name: "missing process metadata",
			change: func(p *observe.Pane) {
				p.Root = observe.Process{}
				p.Foreground = observe.Process{}
			},
		},
		{
			name: "confirmed dead",
			change: func(p *observe.Pane) {
				p.Dead = true
				p.Root = observe.Process{}
			},
		},
		{
			name: "respawn root",
			change: func(p *observe.Pane) {
				p.RootPID++
				p.Root.PID++
			},
			replaced: true,
		},
		{"PID reused", func(p *observe.Pane) { p.Root.Birth = "replacement" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Source{server: "server", epoch: "fixture"}
			facts := sourceFacts()
			before := s.reconcilePane(sourcePane{}, facts, Selection{}, true)
			before.stream = sourceScope(facts)
			tc.change(&facts)

			after := s.reconcilePane(before, facts, Selection{}, true)

			require.Equal(t, tc.replaced, before.pane.Ref != after.pane.Ref)
			if facts.Dead || facts.Root.Identity() == "" || facts.Foreground != before.facts.Foreground {
				require.Empty(t, after.pane.Scope.Connection)
			}
		})
	}
}

func TestSourceRetainsRootIdentityAcrossMissingMetadata(t *testing.T) {
	s := &Source{server: "server", epoch: "fixture"}
	facts := sourceFacts()
	before := s.reconcilePane(sourcePane{}, facts, Selection{}, true)
	unavailable := facts
	unavailable.Root = observe.Process{}
	missing := s.reconcilePane(before, unavailable, Selection{}, false)

	restored := s.reconcilePane(missing, facts, Selection{}, true)
	facts.Root.Birth = "replacement"
	reused := s.reconcilePane(missing, facts, Selection{}, true)
	removedAndReturned := s.reconcilePane(sourcePane{}, facts, Selection{}, true)

	require.Equal(t, before.pane.Ref, restored.pane.Ref)
	require.NotEqual(t, before.pane.Ref, reused.pane.Ref)
	require.NotEqual(t, reused.pane.Ref, removedAndReturned.pane.Ref)
}

func sourceFixture(t *testing.T) (*Source, sourcePane) {
	t.Helper()

	core, err := service.New("server")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, core.Close()) })

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	s := &Source{
		server:   "server",
		epoch:    "fixture",
		core:     core,
		ctx:      ctx,
		cancel:   cancel,
		revision: 1,
	}
	row := s.reconcilePane(sourcePane{}, sourceFacts(), Selection{}, true)
	s.panes = map[string]sourcePane{row.pane.Ref.ID: row}
	err = core.UpdateInventory(service.Inventory{
		Revision:   1,
		ValidUntil: time.Now().Add(time.Minute),
		Panes:      []service.Pane{row.pane},
	})
	require.NoError(t, err)

	return s, row
}

func TestSourceResetRevokesEvidenceBeforeNewOutput(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	old, ok := s.core.Target(row.pane.Ref, service.OSC7501)
	require.True(t, ok)
	require.NoError(t, s.core.Apply(old, 1, status.Value{State: status.Working}))

	scope.Generation++
	s.Reset(scope)
	err := s.core.Apply(old, 2, status.Value{State: status.Idle})
	require.NoError(t, s.Output(scope, []byte("arbitrary terminal text")))
	snapshot, readErr := s.ReadStatus(t.Context())

	require.ErrorIs(t, err, service.ErrStale)
	require.NoError(t, readErr)
	require.Equal(t, status.Unknown, snapshot.At(row.pane.Ref, time.Now()))
	require.Equal(t, row.pane.Ref, s.panes[row.facts.ID].pane.Ref)

	s.Reset(Scope{Pane: row.facts.ID})
	_, available := s.core.Target(row.pane.Ref, service.OSC7501)
	require.False(t, available)
}

func TestSourceBareOutputRemainsUnreported(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)

	require.NoError(t, s.Output(scope, []byte("working, done, error, idle")))
	snapshot, err := s.ReadStatus(t.Context())

	require.NoError(t, err)
	require.Equal(t, status.Unreported, snapshot.At(row.pane.Ref, time.Now()))
}

func TestSourceOutputDirtiesOnlyCurrentSelectedScreen(t *testing.T) {
	s, row := sourceFixture(t)
	s.screens = newScreens(s.core, successfulScreens)
	entry := &screenEntry{}
	s.screens.entries[row.facts.ID] = entry
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, []byte("generic output")))
	require.True(t, entry.firstDirty.IsZero())

	row = s.panes[row.facts.ID]
	row.selection.Profile = service.Profile{Source: service.Screen, Generation: "screen"}
	s.panes[row.facts.ID] = row
	stale := scope
	stale.Generation++

	require.NoError(t, s.Output(stale, []byte("stale output")))
	require.True(t, entry.firstDirty.IsZero())
	require.NoError(t, s.Output(scope, []byte("selected output")))

	require.False(t, entry.firstDirty.IsZero())
}

func TestSourceQueryFailureRevokesTargetsWithoutRetiringPane(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	target, available := s.core.Target(row.pane.Ref, service.OSC7501)
	require.True(t, available)
	require.NoError(t, s.core.Apply(target, 1, status.Value{State: status.Working}))

	err := s.publishLocked(observe.Inventory{})
	snapshot, readErr := s.ReadStatus(t.Context())

	require.NoError(t, err)
	require.NoError(t, readErr)
	require.Equal(t, status.Unknown, snapshot.At(row.pane.Ref, time.Now()))
	require.Equal(t, row.pane.Ref, s.panes[row.facts.ID].pane.Ref)
	require.ErrorIs(t, s.core.Apply(target, 2, status.Value{State: status.Idle}), service.ErrStale)
}

func TestWatchCancellationPreservesTeardownFailure(t *testing.T) {
	failure := errors.New("teardown failed")

	err := unexpectedWatchError(errors.Join(context.Canceled, errors.Join(context.DeadlineExceeded, failure)))

	require.ErrorIs(t, err, failure)
	require.NotErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, context.DeadlineExceeded)
}
