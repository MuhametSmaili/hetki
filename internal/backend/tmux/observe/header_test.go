package observe

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeHeaderEncodingMatchesWireBytes(t *testing.T) {
	cases := []struct {
		name   string
		layout headerLayout
		hasFD  bool
		little [16]byte
		big    [16]byte
	}{
		{
			name: "legacy without descriptor", layout: headerLegacy,
			little: [16]byte{104, 0, 0, 0, 24, 0, 0, 0, 8, 0, 0, 0, 4, 3, 2, 1},
			big:    [16]byte{0, 0, 0, 104, 0, 24, 0, 0, 0, 0, 0, 8, 1, 2, 3, 4},
		},
		{
			name: "legacy with descriptor", layout: headerLegacy, hasFD: true,
			little: [16]byte{104, 0, 0, 0, 24, 0, 1, 0, 8, 0, 0, 0, 4, 3, 2, 1},
			big:    [16]byte{0, 0, 0, 104, 0, 24, 0, 1, 0, 0, 0, 8, 1, 2, 3, 4},
		},
		{
			name: "current without descriptor", layout: headerCurrent,
			little: [16]byte{104, 0, 0, 0, 24, 0, 0, 0, 8, 0, 0, 0, 4, 3, 2, 1},
			big:    [16]byte{0, 0, 0, 104, 0, 0, 0, 24, 0, 0, 0, 8, 1, 2, 3, 4},
		},
		{
			name: "current with descriptor", layout: headerCurrent, hasFD: true,
			little: [16]byte{104, 0, 0, 0, 24, 0, 0, 128, 8, 0, 0, 0, 4, 3, 2, 1},
			big:    [16]byte{0, 0, 0, 104, 128, 0, 0, 24, 0, 0, 0, 8, 1, 2, 3, 4},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.big
			if binary.NativeEndian.Uint16([]byte{1, 0}) == 1 {
				want = tc.little
			}

			header, err := tc.layout.encode(104, 24, 0x01020304, tc.hasFD)

			require.NoError(t, err)
			require.Equal(t, want, header)
		})
	}
}

func TestNativeHeaderSizeBounds(t *testing.T) {
	cases := []struct {
		name  string
		size  uint32
		valid bool
	}{
		{"empty", 0, false},
		{"below header", 15, false},
		{"header only", 16, true},
		{"maximum", 16384, true},
		{"over maximum", 16385, false},
		{"legacy length overflow", 65536, false},
	}
	for _, layout := range []headerLayout{headerLegacy, headerCurrent} {
		for _, tc := range cases {
			t.Run(layoutName(layout)+"/"+tc.name, func(t *testing.T) {
				frame := nativeFrame(layout, 218, tc.size, 8, 0)
				header := [16]byte(frame[:16])

				_, encodeErr := layout.encode(218, tc.size, 1, false)
				_, size, decodeErr := layout.decode(header)

				if tc.valid {
					require.NoError(t, encodeErr)
					require.NoError(t, decodeErr)
					require.Equal(t, tc.size, size)
				} else {
					require.Error(t, encodeErr)
					require.Error(t, decodeErr)
				}
			})
		}
	}
}

func TestNativeHeaderRejectsMissingLayout(t *testing.T) {
	var layout headerLayout
	frame := nativeFrame(headerCurrent, 207, 16, 8, 0)

	_, encodeErr := layout.encode(207, 16, 1, false)
	_, _, decodeErr := layout.decode([16]byte(frame[:16]))

	require.Error(t, encodeErr)
	require.Error(t, decodeErr)
}
