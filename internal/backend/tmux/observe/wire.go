package observe

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// tmux 3.7c/3.8 tmux-protocol.h and compat/imsg.h: protocol 8, a 16-byte
// native-endian header and a 32-bit length whose high bit marks an attached FD.
const (
	nativeProtocol = 8
	nativeMaxFrame = 16384
	nativeReady    = 207
	nativeFlags    = 218
	nativeCommand  = 200

	identifyTerm      = 101
	identifyTTY       = 102
	identifyInput     = 104
	identifyDone      = 106
	identifyPID       = 107
	identifyCWD       = 108
	identifyFeatures  = 109
	identifyOutput    = 110
	identifyLongFlags = 111

	clientControl    = 0x2000
	clientUTF8       = 0x10000
	clientIgnoreSize = 0x20000
	clientNoOutput   = 0x4000000
	clientPauseAfter = 0x100000000
	clientDiscard    = 0x10000000000
	controlFlags     = clientControl | clientUTF8 | clientIgnoreSize
	pausedFlags      = clientNoOutput | clientPauseAfter | clientDiscard
)

func writeNativeMessage(wire *net.UnixConn, kind uint32, body []byte, fd int) error {
	if len(body) > nativeMaxFrame-16 {
		return fmt.Errorf("native tmux message exceeds limit")
	}

	frame := make([]byte, 16+len(body))
	size := uint32(len(frame))
	var rights []byte
	if fd >= 0 {
		size |= 0x80000000
		rights = unix.UnixRights(fd)
	}

	binary.NativeEndian.PutUint32(frame, kind)
	binary.NativeEndian.PutUint32(frame[4:], size)
	binary.NativeEndian.PutUint32(frame[8:], nativeProtocol)
	binary.NativeEndian.PutUint32(frame[12:], uint32(os.Getpid()))
	copy(frame[16:], body)
	n, oobn, err := wire.WriteMsgUnix(frame, rights, nil)
	if err != nil {
		return err
	}

	if oobn != len(rights) {
		return io.ErrShortWrite
	}

	// A partial byte write must not resend the already-transferred descriptors.
	return writeNativeBytes(wire, frame[n:])
}

func writeNativeBytes(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}

		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}

	return nil
}

func readNativeMessage(reader io.Reader) (uint32, error) {
	var header [16]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return 0, err
	}

	kind := binary.NativeEndian.Uint32(header[:])
	size := binary.NativeEndian.Uint32(header[4:])
	if binary.NativeEndian.Uint32(header[8:]) != nativeProtocol || size < 16 || size > nativeMaxFrame {
		return 0, fmt.Errorf("invalid native tmux frame")
	}

	if kind == nativeReady && size == 16 {
		return kind, nil
	}

	if kind != nativeFlags || size != 24 {
		return 0, fmt.Errorf("native tmux control ended with message %d", kind)
	}

	var body [8]byte
	if _, err := io.ReadFull(reader, body[:]); err != nil {
		return 0, err
	}

	flags := binary.NativeEndian.Uint64(body[:])
	required := uint64(clientControl | clientIgnoreSize)
	if flags&required != required || flags&pausedFlags != 0 {
		return 0, fmt.Errorf("native tmux control flags lost output continuity")
	}

	return kind, nil
}

func identifyNative(wire *net.UnixConn, session string, input, output *os.File) error {
	pid := make([]byte, 4)
	binary.NativeEndian.PutUint32(pid, uint32(os.Getpid()))
	flags := make([]byte, 8)
	binary.NativeEndian.PutUint64(flags, controlFlags)
	messages := []struct {
		kind uint32
		body []byte
		fd   int
	}{
		{identifyLongFlags, flags, -1},
		{identifyTerm, []byte("xterm-256color\x00"), -1},
		{identifyFeatures, make([]byte, 4), -1},
		{identifyTTY, []byte(fmt.Sprintf("hetki-status-%d-%s\x00", os.Getpid(), session)), -1},
		{identifyCWD, []byte("/\x00"), -1},
		{identifyInput, nil, int(input.Fd())},
		{identifyOutput, nil, int(output.Fd())},
		{identifyPID, pid, -1},
		{identifyDone, nil, -1},
	}
	for _, message := range messages {
		if err := writeNativeMessage(wire, message.kind, message.body, message.fd); err != nil {
			return err
		}
	}

	args := []string{"attach-session", "-E", "-f", "ignore-size", "-t", session}
	body := make([]byte, 4)
	binary.NativeEndian.PutUint32(body, uint32(len(args)))
	body = append(body, strings.Join(args, "\x00")...)
	body = append(body, 0)
	return writeNativeMessage(wire, nativeCommand, body, -1)
}
