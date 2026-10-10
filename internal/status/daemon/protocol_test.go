package daemon

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

func TestFrameRejectsInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		name string
		size uint32
		body string
	}{
		{"empty", 0, ""},
		{"oversized", maxRequestBytes + 1, ""},
		{"truncated", 20, "{}"},
		{"unknown field", 11, `{"other":1}`},
		{"trailing JSON", 5, `{} {}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var frame bytes.Buffer
			require.NoError(t, binary.Write(&frame, binary.BigEndian, tt.size))
			frame.WriteString(tt.body)
			var req request

			err := readFrame(&frame, maxRequestBytes, &req)

			require.Error(t, err)
		})
	}
}

func TestClientRejectsIncompatibleReply(t *testing.T) {
	valid := reply{Version: protocolVersion, Server: "server", PID: 1, Epoch: strings.Repeat("a", 32)}
	for _, tt := range []struct {
		name   string
		change func(*reply)
		want   error
	}{
		{"version", func(r *reply) { r.Version++ }, ErrProtocol},
		{"server", func(r *reply) { r.Server = "replacement" }, ErrIdentity},
		{"epoch", func(r *reply) { r.Epoch = "invalid" }, ErrIdentity},
		{"pid", func(r *reply) { r.PID = 0 }, ErrIdentity},
	} {
		t.Run(tt.name, func(t *testing.T) {
			row := valid
			tt.change(&row)

			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			require.NoError(t, client.SetDeadline(time.Now().Add(time.Second)))

			done := make(chan struct{})
			go func() {
				defer close(done)

				var req request
				_ = readFrame(server, maxRequestBytes, &req)
				_ = writeFrame(server, maxReplyBytes, row)
			}()

			_, err := (&Client{server: "server"}).exchange(client, "ping")

			require.ErrorIs(t, err, tt.want)
			receive(t, done)
		})
	}
}

func TestDaemonRejectsWrongVersionAndServer(t *testing.T) {
	c := fixtureClient(t, "compatible")
	startFixture(t, c, liveBinding(c.server), staticSource(status.Snapshot{}))
	for _, req := range []request{
		{protocolVersion + 1, c.server, "snapshot"},
		{protocolVersion, "another-server", "snapshot"},
	} {
		conn, err := net.Dial("unix", c.path)
		require.NoError(t, err)
		require.NoError(t, conn.SetDeadline(time.Now().Add(time.Second)))
		require.NoError(t, writeFrame(conn, maxRequestBytes, req))

		var result reply
		err = readFrame(conn, maxReplyBytes, &result)
		_ = conn.Close()

		require.Error(t, err)
	}

	_, err := c.Ping(context.Background())
	require.NoError(t, err)
}

func TestSnapshotRejectsDuplicateInvalidAndOversizedEntries(t *testing.T) {
	valid := entry{ID: "%1", Generation: "birth", State: status.Idle}
	for _, entries := range [][]entry{
		{valid, valid},
		{{ID: "%1", Generation: "birth", State: "done"}},
		{{ID: "%1", State: status.Unknown}},
		{{ID: strings.Repeat("x", maxIdentityBytes+1), Generation: "birth", State: status.Idle}},
		make([]entry, status.MaxPanes+1),
	} {
		_, err := (reply{Server: "server", Entries: entries}).snapshot()

		require.Error(t, err)
	}
}

func TestSnapshotTransportRejectsForeignServer(t *testing.T) {
	snapshot, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		{Server: "other", ID: "%1", Generation: "birth"}: {State: status.Working},
	})
	require.NoError(t, err)

	_, err = snapshotEntries("server", snapshot)

	require.ErrorIs(t, err, ErrIdentity)
}

func FuzzReadFrame(f *testing.F) {
	var seed bytes.Buffer
	_ = writeFrame(&seed, maxRequestBytes, request{protocolVersion, "server", "ping"})
	f.Add(seed.Bytes())
	f.Add([]byte{255, 255, 255, 255})

	f.Fuzz(func(t *testing.T, raw []byte) {
		var req request
		if readFrame(bytes.NewReader(raw), maxRequestBytes, &req) == nil {
			encoded, err := json.Marshal(req)
			require.NoError(t, err)
			require.True(t, json.Valid(encoded))
		}
	})
}
