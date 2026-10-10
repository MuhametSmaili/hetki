package observe

import (
	"bytes"
	"encoding/binary"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
)

func layoutName(layout headerLayout) string {
	if layout == headerLegacy {
		return "legacy"
	}
	return "current"
}

func nativeFrame(layout headerLayout, kind, size, version uint32, flags uint64) []byte {
	frame := make([]byte, 24)
	binary.NativeEndian.PutUint32(frame, kind)
	if layout == headerLegacy {
		binary.NativeEndian.PutUint16(frame[4:], uint16(size))
	} else {
		binary.NativeEndian.PutUint32(frame[4:], size)
	}
	binary.NativeEndian.PutUint32(frame[8:], version)
	binary.NativeEndian.PutUint64(frame[16:], flags)
	return frame
}

func TestNativeWireRejectsLostOutputAndUnsupportedInstructions(t *testing.T) {
	for _, layout := range []headerLayout{headerLegacy, headerCurrent} {
		t.Run(layoutName(layout), func(t *testing.T) {
			testNativeWireMessages(t, layout)
		})
	}
}

func testNativeWireMessages(t *testing.T, layout headerLayout) {
	t.Helper()
	frame := func(kind, size, version uint32, flags uint64) []byte {
		return nativeFrame(layout, kind, size, version, flags)
	}
	withFD := frame(218, 24, 8, 0x32000)
	unknownFlags := frame(218, 24, 8, 0x32000)
	if layout == headerLegacy {
		binary.NativeEndian.PutUint16(withFD[6:], 1)
		binary.NativeEndian.PutUint16(unknownFlags[6:], 2)
	} else {
		binary.NativeEndian.PutUint32(withFD[4:], 0x80000018)
		binary.NativeEndian.PutUint32(unknownFlags[4:], 0x40000018)
	}

	cases := []struct {
		name  string
		frame []byte
		valid bool
	}{
		{"fragmented flags", frame(218, 24, 8, 0x32000), true},
		{"ready", frame(207, 16, 8, 0)[:16], true},
		{"wrong protocol", frame(218, 24, 7, 0x32000), false},
		{"no output", frame(218, 24, 8, 0x4032000), false},
		{"paused output", frame(218, 24, 8, 0x100032000), false},
		{"discarded output", frame(218, 24, 8, 0x10000032000), false},
		{"resize enabled", frame(218, 24, 8, 0x12000), false},
		{"server file request", frame(303, 24, 8, 0), false},
		{"attached descriptor", withFD, false},
		{"reserved bits", unknownFlags, false},
		{"oversized frame", frame(218, 16385, 8, 0), false},
		{"truncated header", frame(218, 24, 8, 0)[:12], false},
		{"truncated flags", frame(218, 24, 8, 0)[:20], false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := iotest.OneByteReader(bytes.NewReader(tc.frame))

			_, err := readNativeMessage(reader, layout)

			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
