package tui

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func windowPreviewModel(t *testing.T, value status.Value) model {
	t.Helper()
	m, panes := windowStatusFixture(t)
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{panes[1].Ref: value})
	require.NoError(t, err)
	m, _ = updateModel(t, m, statusResultMsg{update: StatusUpdate{Values: values}})
	m.mode = modeBrowse
	require.True(t, m.items.Select(list.ItemID{ID: "window"}))
	return m
}

func TestWindowPreviewAndOpenUseTheDisplayedPane(t *testing.T) {
	m := windowPreviewModel(t, status.Value{State: status.Blocked, ValidUntil: time.Now().Add(time.Minute)})
	id := list.ItemID{ID: "window"}
	var captured PreviewRequest
	m.readPreview = func(_ context.Context, request PreviewRequest) (string, error) {
		captured = request
		return "agent screen", nil
	}
	var opened ActionRequest
	m.dispatch = func(request ActionRequest) (ActionResult, error) {
		opened = request
		return ActionResult{}, nil
	}

	m, cmd := updateModel(t, m, previewKey())
	require.NotNil(t, cmd)
	m, _ = updateModel(t, m, cmd())

	require.Equal(t, PreviewRequest{ItemID: id, PaneID: "%2"}, captured)
	row, ok := m.items.Selected()
	require.True(t, ok)
	require.Equal(t, "/agent", row.Item.Secondary)
	require.Equal(t, "opencode", row.Item.Trailing)
	require.Contains(t, terminal.Sanitize(m.View().Content), "agent screen")

	_, cmd = updateModel(t, m, specialKey(tea.KeyEnter))
	require.NotNil(t, cmd)
	cmd()

	require.Equal(t, ActionRequest{ActionID: ActionOpen, ItemID: id, PaneID: "%2"}, opened)
}

func TestWindowPreviewRejectsTheOldPaneAfterRepresentativeExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := windowPreviewModel(t, status.Value{State: status.Unknown, Outcome: status.Completed, OutcomeSince: time.Now(), ValidUntil: time.Now().Add(time.Minute)})
		id := list.ItemID{ID: "window"}
		var requests []PreviewRequest
		var readErrors []error
		m.readPreview = func(ctx context.Context, request PreviewRequest) (string, error) {
			requests = append(requests, request)
			readErrors = append(readErrors, ctx.Err())
			return request.PaneID + " screen", nil // Completed capture can race cancellation.
		}
		m, pending := updateModel(t, m, previewKey())
		require.NotNil(t, pending)

		time.Sleep(statusSuccessDuration)
		m, _ = updateModel(t, m, statusFrameMsg{generation: m.paneStatus.frameGeneration})
		row, ok := m.items.Selected()
		require.True(t, ok)
		require.Equal(t, id, row.Item.ID)
		require.Equal(t, "/shell", row.Item.Secondary)
		m, next := updateModel(t, m, pending())

		require.Equal(t, []PreviewRequest{{ItemID: id, PaneID: "%2"}}, requests)
		require.ErrorIs(t, readErrors[0], context.Canceled)
		require.NotContains(t, terminal.Sanitize(m.View().Content), "%2 screen")
		require.Contains(t, terminal.Sanitize(m.View().Content), "loading")
		require.NotNil(t, next)
		m, _ = updateModel(t, m, next())

		require.Equal(t, []PreviewRequest{{ItemID: id, PaneID: "%2"}, {ItemID: id, PaneID: "%1"}}, requests)
		require.NoError(t, readErrors[1])
		require.Contains(t, terminal.Sanitize(m.View().Content), "%1 screen")
	})
}

func TestWindowPreviewRetainsTheScreenUntilItsPaneChanges(t *testing.T) {
	m := windowPreviewModel(t, status.Value{State: status.Blocked, ValidUntil: time.Now().Add(time.Minute)})
	id := list.ItemID{ID: "window"}
	m.paneStatus.windows[id][0].Ref = status.PaneRef{}
	var requests []PreviewRequest
	m.readPreview = func(_ context.Context, request PreviewRequest) (string, error) {
		requests = append(requests, request)
		return request.PaneID + " screen", nil
	}
	m, cmd := updateModel(t, m, previewKey())
	require.NotNil(t, cmd)
	m, _ = updateModel(t, m, cmd())
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		m.paneStatus.windows[id][1].Ref: {State: status.Unknown, Outcome: status.Failed, ValidUntil: time.Now().Add(time.Minute)},
	})
	require.NoError(t, err)

	m, _ = updateModel(t, m, statusResultMsg{update: StatusUpdate{Values: values}})

	require.True(t, m.preview.ready, "a new status on the same pane must not trigger another capture")
	require.Contains(t, terminal.Sanitize(m.View().Content), "%2 screen")

	m, cmd = updateModel(t, m, statusResultMsg{update: StatusUpdate{}})

	require.Equal(t, []PreviewRequest{{ItemID: id, PaneID: "%2"}}, requests)
	require.NotContains(t, terminal.Sanitize(m.View().Content), "%2 screen")
	require.Contains(t, terminal.Sanitize(m.View().Content), "loading")
	require.NotNil(t, cmd)
	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok)
	for _, effect := range batch {
		m, _ = updateModel(t, m, effect())
	}

	require.Equal(t, []PreviewRequest{{ItemID: id, PaneID: "%2"}, {ItemID: id, PaneID: "%1"}}, requests)
	require.Contains(t, terminal.Sanitize(m.View().Content), "%1 screen")
}
