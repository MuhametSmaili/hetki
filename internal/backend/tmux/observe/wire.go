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

// ClientNamePrefix is reserved for native status control clients.
const ClientNamePrefix = "hetki-status-"

// Message IDs and required client flags are shared by the audited 3.2–3.8
// releases (tmux.h and, in later releases, tmux-protocol.h).
const (
	nativeReady   = 207
	nativeFlags   = 218
	nativeCommand = 200

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

func writeNativeMessage(wire *net.UnixConn, layout headerLayout, kind uint32, body []byte, fd int) error {
	if len(body) > nativeMaxFrame-nativeHeaderSize {
		return fmt.Errorf("native tmux message exceeds limit")
	}

	header, err := layout.encode(kind, uint32(nativeHeaderSize+len(body)), uint32(os.Getpid()), fd >= 0)
	if err != nil {
		return err
	}

	frame := make([]byte, nativeHeaderSize+len(body))
	copy(frame, header[:])
	copy(frame[nativeHeaderSize:], body)
	var rights []byte
	if fd >= 0 {
		rights = unix.UnixRights(fd)
	}

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

func readNativeMessage(reader io.Reader, layout headerLayout) (uint32, error) {
	var header [nativeHeaderSize]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return 0, err
	}

	kind, size, err := layout.decode(header)
	if err != nil {
		return 0, err
	}

	if kind == nativeReady && size == nativeHeaderSize {
		return kind, nil
	}

	if kind != nativeFlags || size != nativeHeaderSize+8 {
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

func identifyNative(wire *net.UnixConn, layout headerLayout, session string, input, output *os.File) error {
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
		{identifyTTY, []byte(fmt.Sprintf("%s%d-%s\x00", ClientNamePrefix, os.Getpid(), session)), -1},
		{identifyCWD, []byte("/\x00"), -1},
		{identifyInput, nil, int(input.Fd())},
		{identifyOutput, nil, int(output.Fd())},
		{identifyPID, pid, -1},
		{identifyDone, nil, -1},
	}
	for _, message := range messages {
		if err := writeNativeMessage(wire, layout, message.kind, message.body, message.fd); err != nil {
			return err
		}
	}

	args := []string{"attach-session", "-E", "-f", "ignore-size", "-t", session}
	body := binary.NativeEndian.AppendUint32(nil, uint32(len(args)))
	body = append(body, strings.Join(args, "\x00")...)
	body = append(body, 0)
	return writeNativeMessage(wire, layout, nativeCommand, body, -1)
}
