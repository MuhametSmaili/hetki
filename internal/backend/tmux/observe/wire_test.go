package observe

import (
	"bytes"
	"encoding/binary"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
)

func nativeFrame(kind, size, version uint32, flags uint64) []byte {
	frame := make([]byte, 24)
	binary.NativeEndian.PutUint32(frame, kind)
	binary.NativeEndian.PutUint32(frame[4:], size)
	binary.NativeEndian.PutUint32(frame[8:], version)
	binary.NativeEndian.PutUint64(frame[16:], flags)
	return frame
}

func TestNativeWireRejectsLostOutputAndUnsupportedInstructions(t *testing.T) {
	cases := []struct {
		name  string
		frame []byte
		valid bool
	}{
		{"fragmented flags", nativeFrame(218, 24, 8, 0x32000), true},
		{"ready", nativeFrame(207, 16, 8, 0)[:16], true},
		{"wrong protocol", nativeFrame(218, 24, 7, 0x32000), false},
		{"no output", nativeFrame(218, 24, 8, 0x4032000), false},
		{"paused output", nativeFrame(218, 24, 8, 0x100032000), false},
		{"discarded output", nativeFrame(218, 24, 8, 0x10000032000), false},
		{"resize enabled", nativeFrame(218, 24, 8, 0x12000), false},
		{"server file request", nativeFrame(303, 24, 8, 0), false},
		{"attached descriptor", nativeFrame(218, 0x80000018, 8, 0), false},
		{"oversized frame", nativeFrame(218, 16385, 8, 0), false},
		{"truncated flags", nativeFrame(218, 24, 8, 0)[:20], false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := iotest.OneByteReader(bytes.NewReader(tc.frame))

			_, err := readNativeMessage(reader)

			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
