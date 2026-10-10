package observe

import (
	"encoding/binary"
	"fmt"
)

const (
	nativeProtocol   = 8
	nativeHeaderSize = 16
	nativeMaxFrame   = 16384
)

// Both bundled imsg layouts use tmux protocol 8 and native byte order. The
// header changed in tmux 3.6, so the protocol number cannot select its layout.
type headerLayout uint8

const (
	headerLegacy  headerLayout = iota + 1 // 3.2–3.5a: uint16 length, uint16 FD flags.
	headerCurrent                         // 3.6–3.8: uint32 length with a high-bit FD marker.
)

func (layout headerLayout) encode(kind, size, pid uint32, hasFD bool) ([nativeHeaderSize]byte, error) {
	var header [nativeHeaderSize]byte
	if size < nativeHeaderSize || size > nativeMaxFrame {
		return header, fmt.Errorf("invalid native tmux frame size %d", size)
	}

	switch layout {
	case headerLegacy:
		binary.NativeEndian.PutUint16(header[4:], uint16(size))
		if hasFD {
			binary.NativeEndian.PutUint16(header[6:], 1)
		}
	case headerCurrent:
		if hasFD {
			size |= 0x80000000
		}
		binary.NativeEndian.PutUint32(header[4:], size)
	default:
		return header, fmt.Errorf("invalid native tmux header layout %d", layout)
	}

	binary.NativeEndian.PutUint32(header[:], kind)
	binary.NativeEndian.PutUint32(header[8:], nativeProtocol)
	binary.NativeEndian.PutUint32(header[12:], pid)
	return header, nil
}

// decode accepts only descriptor-free replies; the observer never receives
// files or handles server-requested file operations.
func (layout headerLayout) decode(header [nativeHeaderSize]byte) (kind, size uint32, err error) {
	switch layout {
	case headerLegacy:
		size = uint32(binary.NativeEndian.Uint16(header[4:]))
		if binary.NativeEndian.Uint16(header[6:]) != 0 {
			return 0, 0, fmt.Errorf("unexpected native tmux header flags")
		}
	case headerCurrent:
		size = binary.NativeEndian.Uint32(header[4:])
		if size&0x80000000 != 0 {
			return 0, 0, fmt.Errorf("unexpected native tmux descriptor")
		}
	default:
		return 0, 0, fmt.Errorf("invalid native tmux header layout %d", layout)
	}

	if binary.NativeEndian.Uint32(header[8:]) != nativeProtocol || size < nativeHeaderSize || size > nativeMaxFrame {
		return 0, 0, fmt.Errorf("invalid native tmux frame")
	}

	return binary.NativeEndian.Uint32(header[:]), size, nil
}
