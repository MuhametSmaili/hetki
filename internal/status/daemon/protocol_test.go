package daemon

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"maps"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
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
	valid := reply{Version: protocolVersion, Server: "server", PID: 1, Epoch: strings.Repeat("a", 32), Build: fixtureBuild}
	for _, tt := range []struct {
		name   string
		change func(*reply)
		want   error
	}{
		{"server", func(r *reply) { r.Server = "replacement" }, ErrIdentity},
		{"epoch", func(r *reply) { r.Epoch = "invalid" }, ErrIdentity},
		{"pid", func(r *reply) { r.PID = 0 }, ErrIdentity},
		{"missing build", func(r *reply) { r.Build = "" }, ErrIdentity},
		{"oversized executable", func(r *reply) { r.Executable = strings.Repeat("x", maxPathBytes+1) }, ErrIdentity},
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

func TestClientReportsDaemonProtocolMismatch(t *testing.T) {
	row := reply{Version: protocolVersion + 1, Server: "server", PID: 4321, Epoch: strings.Repeat("a", 32), Error: protocolMismatch}
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

	var mismatch *ProtocolMismatchError
	require.ErrorAs(t, err, &mismatch)
	require.Equal(t, ProtocolMismatchError{PID: 4321, Epoch: strings.Repeat("a", 32), Version: protocolVersion + 1}, *mismatch)
	require.ErrorIs(t, err, ErrProtocol)
	receive(t, done)
}

func TestPingReportsDaemonThatClosesWithoutReplying(t *testing.T) {
	c := fixtureClient(t, "no-reply")
	listener, cleanup, err := listen(c.path)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.NoError(t, listener.SetDeadline(time.Now().Add(3*time.Second)))
	done := make(chan struct{})
	go func() {
		defer close(done)

		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		var req request
		_ = readFrame(conn, maxRequestBytes, &req)
		_ = conn.Close()
	}()

	_, err = c.Ping(t.Context())

	require.ErrorIs(t, err, errNoReply)
	require.ErrorIs(t, err, io.EOF)
	receive(t, done)
}

func TestDaemonRepliesToOtherProtocolVersions(t *testing.T) {
	c := fixtureClient(t, "protocol-mismatch")
	startFixture(t, c, liveBinding(c.server), staticSource(status.Snapshot{}))
	identity, err := c.Ping(t.Context())
	require.NoError(t, err)
	for _, tc := range []struct {
		name  string
		frame map[string]any
	}{
		{"older client", map[string]any{"version": protocolVersion - 1, "server": c.server, "action": "ping"}},
		{"newer client with unknown fields", map[string]any{"version": protocolVersion + 1, "server": c.server, "action": "ping", "future": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := net.Dial("unix", c.path)
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, conn.SetDeadline(time.Now().Add(time.Second)))
			require.NoError(t, writeFrame(conn, maxRequestBytes, tc.frame))

			raw, err := readFrameData(conn, maxReplyBytes)
			var fields map[string]json.RawMessage
			fieldsErr := json.Unmarshal(raw, &fields)
			var result reply
			decodeErr := decodeFrame(raw, &result)

			require.NoError(t, err)
			require.NoError(t, fieldsErr)
			// Clients of every version identify and stop this instance with these fields only.
			require.ElementsMatch(t, []string{"version", "server", "pid", "epoch", "error"}, slices.Collect(maps.Keys(fields)))
			require.NoError(t, decodeErr)
			require.Equal(t, reply{
				Version: protocolVersion, Server: c.server, PID: identity.PID, Epoch: identity.Epoch, Error: protocolMismatch,
			}, result)
		})
	}

	_, err = c.Ping(t.Context())
	require.NoError(t, err)
}

func TestDaemonRejectsForeignServerWithoutReplying(t *testing.T) {
	c := fixtureClient(t, "compatible")
	startFixture(t, c, liveBinding(c.server), staticSource(status.Snapshot{}))
	conn, err := net.Dial("unix", c.path)
	require.NoError(t, err)
	require.NoError(t, conn.SetDeadline(time.Now().Add(time.Second)))
	require.NoError(t, writeFrame(conn, maxRequestBytes, request{Version: protocolVersion, Server: "another-server", Action: "subscribe"}))

	var result reply
	err = readFrame(conn, maxReplyBytes, &result)
	_ = conn.Close()

	require.ErrorIs(t, err, io.EOF)
	_, err = c.Ping(context.Background())
	require.NoError(t, err)
}

func TestSnapshotRejectsDuplicateInvalidAndOversizedEntries(t *testing.T) {
	valid := entry{ID: "%1", Generation: "birth", State: status.Idle}
	for _, entries := range [][]entry{
		{valid, valid},
		{{ID: "%1", Generation: "birth", State: "done"}},
		{{ID: "%1", Generation: "birth", State: status.Unknown, Outcome: "future-result"}},
		{{ID: "%1", Generation: "birth", State: status.Working, Outcome: status.Completed}},
		{{ID: "%1", Generation: "birth", State: status.Unknown, OutcomeEnded: true}},
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

func TestOutcomeSurvivesSocketTransport(t *testing.T) {
	c := fixtureClient(t, "outcomes")
	observed := time.Now().Add(-3 * time.Second)
	deadline := observed.Add(time.Minute)
	values := make(map[status.PaneRef]status.Value)
	for _, tc := range []struct {
		name    string
		outcome status.Outcome
		ended   bool
	}{
		{"foreground success", status.Completed, false},
		{"ended success", status.Completed, true},
		{"foreground error", status.Failed, false},
		{"ended error", status.Failed, true},
	} {
		pane := status.PaneRef{Server: c.server, ID: tc.name, Generation: "birth"}
		values[pane] = status.Value{
			State: status.Unknown, Outcome: tc.outcome, OutcomeSince: observed,
			OutcomeEnded: tc.ended, ValidUntil: deadline,
		}
	}
	snapshot, err := status.NewSnapshot(values)
	require.NoError(t, err)
	startFixture(t, c, liveBinding(c.server), staticSource(snapshot))

	got, err := readFirst(t.Context(), c)

	require.NoError(t, err)
	for pane, value := range values {
		require.Equal(t, value.Outcome, got.Lookup(pane).OutcomeAt(observed.Add(time.Second)))
		require.Equal(t, status.Unknown, got.At(pane, observed.Add(time.Second)))
		require.True(t, deadline.Equal(got.Lookup(pane).ValidUntil))
		require.True(t, observed.Equal(got.Lookup(pane).OutcomeSince))
		require.Equal(t, value.OutcomeEnded, got.Lookup(pane).OutcomeEnded)
	}
}

func FuzzReadFrame(f *testing.F) {
	var seed bytes.Buffer
	_ = writeFrame(&seed, maxRequestBytes, request{Version: protocolVersion, Server: "server", Action: "ping"})
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
