package status_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

func TestValueFreshness(t *testing.T) {
	deadline := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, state := range []status.State{status.Idle, status.Working, status.Blocked} {
		t.Run(string(state), func(t *testing.T) {
			value := status.Value{State: state, ValidUntil: deadline}
			require.Equal(t, state, value.At(deadline.Add(-time.Nanosecond)))
			require.Equal(t, status.Unknown, value.At(deadline))
			require.Equal(t, status.Unknown, value.At(deadline.Add(time.Hour)))
			require.Equal(t, status.Unknown, (status.Value{State: state}).At(deadline))
		})
	}
	for _, state := range []status.State{status.Unreported, status.Withdrawn, status.Unknown} {
		require.Equal(t, state, (status.Value{State: state}).At(deadline))
		require.Equal(t, state, (status.Value{State: state, ValidUntil: deadline}).At(deadline.Add(time.Hour)))
	}
	require.Equal(t, status.Unknown, (status.Value{State: "future-state", ValidUntil: deadline}).At(deadline.Add(-time.Hour)))
}

func TestOutcomeFreshnessDoesNotImplyIdle(t *testing.T) {
	deadline := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, outcome := range []status.Outcome{status.Completed, status.Failed} {
		t.Run(string(outcome), func(t *testing.T) {
			value := status.Value{State: status.Unknown, Outcome: outcome, ValidUntil: deadline}
			pane := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
			snapshot, err := status.NewSnapshot(map[status.PaneRef]status.Value{pane: value})
			require.NoError(t, err)

			require.Equal(t, status.Unknown, value.At(deadline.Add(-time.Second)))
			require.Equal(t, outcome, value.OutcomeAt(deadline.Add(-time.Second)))
			require.Equal(t, status.NoOutcome, value.OutcomeAt(deadline))
			require.Equal(t, status.NoOutcome, snapshot.Invalidate().Lookup(pane).OutcomeAt(deadline.Add(-time.Second)))
			value.ValidUntil = time.Time{}
			require.Equal(t, status.NoOutcome, value.OutcomeAt(deadline))
		})
	}
}

func TestSnapshotRejectsInvalidOutcomeCombinations(t *testing.T) {
	pane := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	for _, value := range []status.Value{
		{State: status.Unknown, Outcome: "future-result"},
		{State: status.Working, Outcome: status.Completed},
		{State: status.Idle, Outcome: status.Completed},
		{State: status.Withdrawn, Outcome: status.Failed},
		{State: status.Idle, OutcomeSince: time.Now()},
	} {
		t.Run(string(value.State)+"/"+string(value.Outcome), func(t *testing.T) {
			_, err := status.NewSnapshot(map[status.PaneRef]status.Value{pane: value})

			require.Error(t, err)
		})
	}
}

func TestSnapshotIdentityAndOwnership(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	pane := status.PaneRef{Server: "server-1", ID: "%1", Generation: "birth-1"}
	values := map[status.PaneRef]status.Value{
		pane: {State: status.Working, ValidUntil: now.Add(time.Minute)},
	}
	snapshot, err := status.NewSnapshot(values)
	require.NoError(t, err)
	values[pane] = status.Value{State: status.Withdrawn}
	delete(values, pane)
	require.Equal(t, status.Working, snapshot.At(pane, now))
	for _, other := range []status.PaneRef{
		{Server: "server-2", ID: pane.ID, Generation: pane.Generation},
		{Server: pane.Server, ID: "%2", Generation: pane.Generation},
		{Server: pane.Server, ID: pane.ID, Generation: "birth-2"},
		{},
	} {
		require.Equal(t, status.Unreported, snapshot.At(other, now))
	}
	require.Equal(t, status.Unreported, (status.Snapshot{}).At(pane, now))
	require.Equal(t, status.Unknown, snapshot.At(pane, now.Add(time.Minute)))
}

func TestSnapshotInvalidationPreservesAbsenceAndWithdrawal(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	values := make(map[status.PaneRef]status.Value)
	for _, state := range []status.State{status.Idle, status.Working, status.Blocked, status.Unknown, status.Withdrawn} {
		pane := status.PaneRef{Server: "server", ID: string(state), Generation: "birth"}
		values[pane] = status.Value{State: state, ValidUntil: now.Add(time.Minute)}
	}
	snapshot, err := status.NewSnapshot(values)
	require.NoError(t, err)
	invalid := snapshot.Invalidate()
	for pane, value := range values {
		want := status.Unknown
		if value.State == status.Withdrawn {
			want = status.Withdrawn
		}
		require.Equal(t, want, invalid.At(pane, now))
		require.Equal(t, want, invalid.Invalidate().At(pane, now))
		require.Equal(t, value.State, snapshot.At(pane, now), "invalidation must not mutate an existing snapshot")
	}
	absent := status.PaneRef{Server: "server", ID: "absent", Generation: "birth"}
	require.Equal(t, status.Unreported, invalid.At(absent, now))
}

func TestSnapshotRejectsInvalidEntriesAtomically(t *testing.T) {
	pane := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	for _, test := range []struct {
		name  string
		pane  status.PaneRef
		state status.State
	}{
		{"missing server", status.PaneRef{ID: pane.ID, Generation: pane.Generation}, status.Working},
		{"missing pane", status.PaneRef{Server: pane.Server, Generation: pane.Generation}, status.Working},
		{"missing generation", status.PaneRef{Server: pane.Server, ID: pane.ID}, status.Working},
		{"unreported entry", pane, status.Unreported},
		{"unsupported state", pane, "done"},
	} {
		t.Run(test.name, func(t *testing.T) {
			valid := status.PaneRef{Server: "server", ID: "%2", Generation: "birth"}
			snapshot, err := status.NewSnapshot(map[status.PaneRef]status.Value{
				valid:     {State: status.Unknown},
				test.pane: {State: test.state},
			})
			require.Error(t, err)
			require.Equal(t, status.Unreported, snapshot.At(valid, time.Time{}))
		})
	}
}

func TestSnapshotPaneLimit(t *testing.T) {
	values := make(map[status.PaneRef]status.Value, status.MaxPanes+1)
	for i := range status.MaxPanes {
		values[status.PaneRef{Server: "server", ID: fmt.Sprint(i), Generation: "birth"}] = status.Value{State: status.Unknown}
	}
	_, err := status.NewSnapshot(values)
	require.NoError(t, err)
	values[status.PaneRef{Server: "server", ID: "overflow", Generation: "birth"}] = status.Value{State: status.Unknown}
	_, err = status.NewSnapshot(values)
	require.ErrorContains(t, err, "exceeds")
}

func BenchmarkSnapshotAt(b *testing.B) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	pane := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	snapshot, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		pane: {State: status.Working, ValidUntil: now.Add(time.Minute)},
	})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		snapshot.At(pane, now)
	}
}
