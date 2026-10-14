package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/daemon"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/MSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestCachedLocationsRebindLinkedRowsAndRespawns(t *testing.T) {
	state := linkedPaneState()
	adapter := testAdapter(&stubBackend{state: state})
	_, err := adapter.Load(t.Context())
	require.NoError(t, err)
	server := observe.Server{PID: 123, Birth: "fixture", Started: 1}
	adapter.statusServer = server
	ref := status.PaneRef{Server: server.Identity(), ID: "%1", Generation: "respawned"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{ref: {State: status.Blocked, ValidUntil: time.Now().Add(time.Minute)}})
	require.NoError(t, err)
	view := daemon.View{Values: values, Locations: []daemon.Location{
		{Session: "$1", Window: "@1", Pane: "%1"},
		{Session: "$2", Window: "@1", Pane: "%1"},
	}}

	bindings := adapter.bindStatusView(view)

	require.Equal(t, ui.StatusBindings{
		windowItemID("$1", "@1"): {ref}, windowItemID("$2", "@1"): {ref},
	}, bindings)
	_, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})
	require.NoError(t, err)
	adapter.statusServer = server
	flat := adapter.bindStatusView(view)
	require.Equal(t, ui.StatusBindings{
		destinationItemID("$1", "@1", "%1"): {ref}, destinationItemID("$2", "@1", "%1"): {ref},
	}, flat)
	view.Locations = []daemon.Location{{Session: "$1", Window: "@99", Pane: "%1"}}
	require.Empty(t, adapter.bindStatusView(view), "moved pane must not bind its old window occurrence")
	view.Locations = []daemon.Location{{Session: "$1", Window: "@1", Pane: "%1"}}
	adapter.statusServer.Birth = "replacement server"
	require.Empty(t, adapter.bindStatusView(view), "reused backend IDs must not bind another server's evidence")
}

type openingStatusDriver struct {
	switchDriver
	err    error
	opened bool
}

func (d *openingStatusDriver) OpenStatus(context.Context) (StatusReader, error) {
	d.opened = true
	return nil, d.err
}

func TestStatusStartupFailureKeepsPickerUsable(t *testing.T) {
	driver := &openingStatusDriver{err: errors.New("invalid profile fixture.yaml")}
	service := Service{Driver: driver, RunUI: func(_ context.Context, snapshot list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		require.True(t, driver.opened)
		require.Contains(t, snapshot.Notice, "invalid profile fixture.yaml")
		require.Nil(t, options.Read)
		return list.ItemID{}, nil
	}}

	err := service.Run(t.Context())

	require.NoError(t, err)
}

type failingViewReader struct {
	statusReaderFunc
	err error
}

func (r failingViewReader) ReadView(context.Context) (daemon.View, error) {
	return daemon.View{}, r.err
}

func TestViewFailureInvalidatesEvidenceAndPreservesBindingOwnership(t *testing.T) {
	ref := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	previous, err := status.NewSnapshot(map[status.PaneRef]status.Value{ref: {State: status.Working, ValidUntil: time.Now().Add(time.Minute)}})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	failure := errors.New("daemon gone")
	effects := uiEffects{ctx: ctx, cancel: cancel, reader: failingViewReader{err: failure}}

	update := effects.readStatus(ctx, previous)

	require.Equal(t, status.Unknown, update.Values.At(ref, time.Now()))
	require.Nil(t, update.Bindings)
	require.ErrorIs(t, update.Err, failure)
}

type replacedServerBackend struct {
	stubBackend
}

func (b *replacedServerBackend) QueryStatusState(context.Context) (backend.StateResult, observe.Server, error) {
	return backend.StateResult{}, observe.Server{}, observe.ErrServerChanged
}

func TestOpeningStatusDefersDaemonWorkUntilRead(t *testing.T) {
	adapter := &LiveAdapter{statusServer: observe.Server{PID: -1}}

	reader, err := adapter.OpenStatus(t.Context())

	require.NoError(t, err, "opening must not validate or launch the daemon")
	require.NotNil(t, reader)
	_, err = reader.ReadStatus(t.Context())
	require.Error(t, err, "the asynchronous read validates the descriptor")
}

func TestServerReplacementDuringLoadDisablesStatusBinding(t *testing.T) {
	b := &replacedServerBackend{stubBackend: stubBackend{state: liveState()}}
	adapter := newLiveAdapter(func(...string) (backend.Backend, error) { return b, nil }, nil, nil)

	_, loadErr := adapter.Load(t.Context())
	reader, statusErr := adapter.OpenStatus(t.Context())

	require.NoError(t, loadErr, "browsing does not depend on status startup")
	require.Nil(t, reader)
	require.ErrorIs(t, statusErr, observe.ErrServerChanged)
}

type blockingViewWatcher struct {
	failingViewReader
	started chan struct{}
}

func (r blockingViewWatcher) WatchView(ctx context.Context) (daemon.View, error) {
	close(r.started)
	<-ctx.Done()
	return daemon.View{}, ctx.Err()
}

func TestPickerShutdownCancelsAndJoinsOutstandingWatch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := blockingViewWatcher{started: make(chan struct{})}
	effects := uiEffects{ctx: ctx, cancel: cancel, reader: reader}
	options := effects.statusOptions()
	finished := make(chan ui.StatusUpdate, 1)
	go func() { finished <- options.Read(ctx, status.Snapshot{}) }()
	awaitChannel(t, reader.started)

	effects.stop()
	update := awaitChannel(t, finished)

	require.Equal(t, daemon.WatchTimeout+time.Second, options.ReadTimeout)
	require.NoError(t, update.Err, "picker cancellation stays quiet")
	require.False(t, update.Watching)
}
