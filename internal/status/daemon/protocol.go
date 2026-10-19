package daemon

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
	"unicode/utf8"

	"github.com/MuhametSmaili/hetki/internal/status"
)

const (
	protocolVersion  = 1
	maxRequestBytes  = 4096
	maxReplyBytes    = 1 << 20
	maxIdentityBytes = 256
	requestTimeout   = 2 * time.Second
)

var (
	ErrProtocol    = errors.New("status daemon protocol mismatch")
	ErrIdentity    = errors.New("status daemon identity mismatch")
	ErrUnavailable = errors.New("status source unavailable")
	ErrLifecycle   = errors.New("status daemon does not support instance-safe lifecycle control")
	ErrTransition  = errors.New("status daemon owns its lock but is not listening; startup or shutdown is in progress")
)

type request struct {
	Version       int     `json:"version"`
	Server        string  `json:"server"`
	Action        string  `json:"action"`
	After         *Cursor `json:"after,omitempty"`
	ExpectedEpoch string  `json:"expected_epoch,omitempty"`
}

type reply struct {
	Version   int        `json:"version"`
	Server    string     `json:"server"`
	PID       int        `json:"pid"`
	Epoch     string     `json:"epoch"`
	Entries   []entry    `json:"entries,omitempty"`
	Locations []Location `json:"locations,omitempty"`
	Error     string     `json:"error,omitempty"`
	Revision  uint64     `json:"revision,omitempty"`
	Watch     bool       `json:"watch,omitempty"`
}

type entry struct {
	ID           string         `json:"id"`
	Generation   string         `json:"generation"`
	State        status.State   `json:"state"`
	Outcome      status.Outcome `json:"outcome,omitempty"`
	OutcomeSince time.Time      `json:"outcome_since,omitzero"`
	ValidUntil   time.Time      `json:"valid_until"`
}

func readFrame(r io.Reader, limit int, value any) error {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}

	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > uint32(limit) {
		return fmt.Errorf("invalid status frame size %d", size)
	}

	raw := make([]byte, int(size))
	if _, err := io.ReadFull(r, raw); err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}

	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing status frame data")
	}

	return nil
}

func writeFrame(w io.Writer, limit int, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}

	if len(raw) > limit {
		return fmt.Errorf("status frame exceeds %d bytes", limit)
	}

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(raw)))
	buffers := net.Buffers{header[:], raw}
	n, err := buffers.WriteTo(w)
	if err == nil && n != int64(len(header)+len(raw)) {
		return io.ErrShortWrite
	}

	return err
}

func snapshotEntries(server string, snapshot status.Snapshot) ([]entry, error) {
	entries := make([]entry, 0, snapshot.Len())
	for pane, value := range snapshot.All() {
		if pane.Server != server {
			return nil, ErrIdentity
		}

		if !validIdentity(pane.ID) || !validIdentity(pane.Generation) {
			return nil, fmt.Errorf("invalid pane identity")
		}

		entries = append(entries, entry{
			ID:           pane.ID,
			Generation:   pane.Generation,
			State:        value.State,
			Outcome:      value.Outcome,
			OutcomeSince: value.OutcomeSince,
			ValidUntil:   value.ValidUntil,
		})
	}

	return entries, nil
}

func (r reply) snapshot() (status.Snapshot, error) {
	if len(r.Entries) > status.MaxPanes {
		return status.Snapshot{}, fmt.Errorf("too many daemon status entries")
	}

	values := make(map[status.PaneRef]status.Value, len(r.Entries))
	for _, e := range r.Entries {
		pane := status.PaneRef{Server: r.Server, ID: e.ID, Generation: e.Generation}
		if !validIdentity(e.ID) || !validIdentity(e.Generation) {
			return status.Snapshot{}, fmt.Errorf("invalid daemon pane identity")
		}

		if _, exists := values[pane]; exists {
			return status.Snapshot{}, fmt.Errorf("duplicate daemon pane identity")
		}

		values[pane] = status.Value{State: e.State, Outcome: e.Outcome, OutcomeSince: e.OutcomeSince, ValidUntil: e.ValidUntil}
	}

	return status.NewSnapshot(values)
}

func validEpoch(value string) bool {
	if len(value) != 32 {
		return false
	}
	epoch, err := hex.DecodeString(value)
	return err == nil && len(epoch) == 16
}

func validIdentity(value string) bool {
	return value != "" && len(value) <= maxIdentityBytes && utf8.ValidString(value)
}
