package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestInspectMissingEndpointDoesNotCreateDirectory(t *testing.T) {
	testenv.PrivateHome(t)

	result, err := Inspect(t.Context(), "missing-service")

	require.NoError(t, err)
	require.Nil(t, result.Instance)
	require.NotEmpty(t, result.LogPath)
	_, err = os.Lstat(filepath.Dir(result.LogPath))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestInspectRunningDaemonPreservesItsInstance(t *testing.T) {
	c := fixtureClient(t, "inspect-running")
	startFixture(t, c, liveBinding(c.server), staticSource(status.Snapshot{}))
	before, err := c.Ping(t.Context())
	require.NoError(t, err)

	result, err := Inspect(t.Context(), c.server)
	after, pingErr := c.Ping(t.Context())

	require.NoError(t, err)
	require.NotNil(t, result.Instance)
	require.Equal(t, before, *result.Instance)
	require.Equal(t, c.LogPath(), result.LogPath)
	require.NoError(t, pingErr)
	require.Equal(t, before, after)
}

func TestInspectStaleSocketDoesNotRemoveIt(t *testing.T) {
	c := fixtureClient(t, "inspect-stale")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: c.path, Net: "unix"})
	require.NoError(t, err)
	listener.SetUnlinkOnClose(false)
	require.NoError(t, listener.Close())
	require.NoError(t, os.Chmod(c.path, 0600))
	before, err := os.Lstat(c.path)
	require.NoError(t, err)

	result, err := Inspect(t.Context(), c.server)
	after, statErr := os.Lstat(c.path)

	require.NoError(t, err)
	require.Nil(t, result.Instance)
	require.NoError(t, statErr)
	require.True(t, os.SameFile(before, after))
}

func TestInspectRejectsUnsafeEndpointWithoutChangingIt(t *testing.T) {
	c := fixtureClient(t, "inspect-unsafe")
	require.NoError(t, os.WriteFile(c.path, []byte("keep"), 0600))

	result, err := Inspect(t.Context(), c.server)
	data, readErr := os.ReadFile(c.path)

	require.ErrorContains(t, err, "unsafe status daemon endpoint")
	require.Nil(t, result.Instance)
	require.NoError(t, readErr)
	require.Equal(t, "keep", string(data))
}

func TestInspectHonorsCanceledContext(t *testing.T) {
	dir := testenv.PrivateHome(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := Inspect(ctx, "canceled-inspection")
	files, readErr := os.ReadDir(dir)

	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, readErr)
	require.Empty(t, files)
}

func TestInspectDoesNotTreatInvalidReplyAsAbsence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*reply)
		want   error
	}{
		{"incompatible protocol", func(r *reply) { r.Version++ }, ErrProtocol},
		{"wrong instance", func(r *reply) { r.Epoch = "invalid" }, ErrIdentity},
		{"unavailable response", func(r *reply) { r.Error = "unavailable" }, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, "inspect-invalid")
			listener, cleanup, err := listen(c.path)
			require.NoError(t, err)
			t.Cleanup(cleanup)
			require.NoError(t, listener.SetDeadline(time.Now().Add(3*time.Second)))
			response := reply{Version: protocolVersion, Server: c.server, PID: 1, Epoch: "0123456789abcdef0123456789abcdef"}
			tc.change(&response)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			var inspectErr error
			go func() {
				defer close(done)
				_, inspectErr = Inspect(ctx, c.server)
			}()
			t.Cleanup(func() {
				cancel()
				receive(t, done)
			})
			conn, err := listener.AcceptUnix()
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, conn.SetDeadline(time.Now().Add(time.Second)))
			var req request
			require.NoError(t, readFrame(conn, maxRequestBytes, &req))

			require.NoError(t, writeFrame(conn, maxReplyBytes, response))
			receive(t, done)

			require.ErrorIs(t, inspectErr, tc.want)
		})
	}
}
