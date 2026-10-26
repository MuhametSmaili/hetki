package daemon

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerPID returns the PID that the kernel records for the socket's peer.
func peerPID(conn net.Conn) (int, error) {
	return socketOption(conn, func(fd int) (int, error) {
		return unix.GetsockoptInt(fd, unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	})
}
