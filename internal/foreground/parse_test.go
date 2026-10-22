package foreground

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func darwinArgs(argc uint32, executable string, pointerSize int, argv ...string) []byte {
	buf := binary.NativeEndian.AppendUint32(nil, argc)
	buf = append(buf, executable...)
	buf = append(buf, 0)
	for (len(buf)-4)%pointerSize != 0 {
		buf = append(buf, 0)
	}
	for _, arg := range argv {
		buf = append(buf, arg...)
		buf = append(buf, 0)
	}
	return buf
}

func TestParseDarwinArgvZeroUsesExactAlignment(t *testing.T) {
	for _, pointerSize := range []int{4, 8} {
		for length := 1; length <= 80; length++ {
			executable := strings.Repeat("x", length)
			buf := darwinArgs(2, executable, pointerSize, "unlisted-cli", "ignored")
			require.Equal(t, "unlisted-cli", parseDarwinArgvZero(buf, pointerSize))
			// An empty argv-zero is not alignment padding. Never promote a
			// later argument, even when it happens to look like a tool name.
			buf = darwinArgs(2, executable, pointerSize, "", "unlisted-secret")
			require.Empty(t, parseDarwinArgvZero(buf, pointerSize))
			buf = darwinArgs(3, executable, pointerSize, "", "", "")
			buf = append(buf, []byte("unlisted-secret\x00")...)
			require.Empty(t, parseDarwinArgvZero(buf, pointerSize))
		}
	}
}

func TestParseDarwinArgvZeroIgnoresArgumentsAndEnvironment(t *testing.T) {
	for _, pointerSize := range []int{4, 8} {
		buf := darwinArgs(3, "/usr/bin/node", pointerSize, "unlisted-cli", "", "")
		buf = append(buf, []byte("SECRET=other-tool\x00")...)
		require.Equal(t, "unlisted-cli", parseDarwinArgvZero(buf, pointerSize))
		for _, script := range []string{
			"/opt/node_modules/@mariozechner/pi-coding-agent/dist/bundle/cli.js",
			"/opt/node_modules/opencode-ai/bin/opencode",
			"/opt/node_modules/unlisted-cli/dist/cli.js",
		} {
			buf := darwinArgs(2, "/usr/bin/node", pointerSize, "node", script)
			buf = append(buf, []byte("SECRET=unlisted-cli\x00")...)
			require.Equal(t, "node", processProgramName(parseDarwinArgvZero(buf, pointerSize)), "script paths must not determine identity")
		}
	}
}

func TestParseDarwinArgvZeroRejectsMalformedOrCapacitySizedData(t *testing.T) {
	valid := darwinArgs(1, "/usr/bin/node", 8, "unlisted-cli")
	for _, size := range []int{maxProgramBytes, maxProgramBytes + 1} {
		buf := append([]byte(nil), valid...)
		buf = append(buf, make([]byte, size-len(buf))...)
		require.Empty(t, parseDarwinArgvZero(buf, 8))
	}
	for _, pointerSize := range []int{-1, 0, 1, 16} {
		require.Empty(t, parseDarwinArgvZero(valid, pointerSize))
	}
	for _, argc := range []uint32{0, 129, ^uint32(0)} {
		require.Empty(t, parseDarwinArgvZero(darwinArgs(argc, "/usr/bin/node", 8, "unlisted-cli"), 8))
	}
	for _, buf := range [][]byte{
		nil, {1, 2, 3},
		darwinArgs(1, "", 8, "unlisted-cli"),
		darwinArgs(1, strings.Repeat("x", 1025), 8, "unlisted-cli"),
		valid[:len(valid)-1], // missing argv-zero terminator
	} {
		require.Empty(t, parseDarwinArgvZero(buf, 8))
	}
	buf := darwinArgs(1, "x", 8, "unlisted-cli")
	buf[6] = 'x' // nonzero byte in alignment padding
	require.Empty(t, parseDarwinArgvZero(buf, 8))
	for length := 4; length < 12; length++ {
		require.Empty(t, parseDarwinArgvZero(darwinArgs(1, "x", 8, "unlisted-cli")[:length], 8))
	}
}

func FuzzParseDarwinArgvZero(f *testing.F) {
	f.Add(darwinArgs(2, "/usr/bin/node", 8, "unlisted-cli", "ignored"), 8)
	f.Add(darwinArgs(2, "/usr/bin/node", 4, "", "secret"), 4)
	f.Add([]byte{}, 0)
	f.Fuzz(func(t *testing.T, buf []byte, pointerSize int) {
		name := parseDarwinArgvZero(buf, pointerSize)
		if name != "" {
			require.True(t, pointerSize == 4 || pointerSize == 8)
			require.Less(t, len(buf), maxProgramBytes)
		}
	})
}
