package list

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPreparationIsolatedFromLiveExpansionAndSelection(t *testing.T) {
	source, err := New(nestedFixture())
	require.NoError(t, err)
	source.DeferSearch()
	before := source.Rows()
	started, resume := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
	})
	type outcome struct {
		model Model
		err   error
	}
	finished := make(chan outcome, 1)
	go func(source Model) {
		prepared, err := source.Prepare(t.Context(), func(items *Model) error {
			close(started)
			<-resume
			return items.Replace(nestedFixture(), ItemID{ID: "editor"})
		})
		finished <- outcome{prepared, err}
	}(source)
	receivePreparation(t, started)

	source.ToggleSelected(false)
	close(resume)
	result := receivePreparation(t, finished)

	require.NoError(t, result.err)
	require.Equal(t, before, result.model.Rows())
	require.Equal(t, ItemID{ID: "editor"}, selectedID(result.model))
	require.Less(t, source.Len(), result.model.Len())
	source.ExpandAll()
	result.model.CollapseAll()
	require.Equal(t, before, source.Rows())
}

func receivePreparation[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for preparation")
		var zero T
		return zero
	}
}

func TestCancelledPreparationPublishesNothingAndLeavesSourceUsable(t *testing.T) {
	source, err := New(nestedFixture())
	require.NoError(t, err)
	before := source.Rows()
	ctx, cancel := context.WithCancel(t.Context())

	prepared, err := source.Prepare(ctx, func(items *Model) error {
		items.CollapseAll()
		cancel()
		return items.Replace(flatFixture(), ItemID{})
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, prepared.Rows())
	require.Equal(t, before, source.Rows())
	source.SetQuery("editor")
	require.Equal(t, ItemID{ID: "editor"}, selectedID(source))
}
