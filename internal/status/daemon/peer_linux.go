package daemon

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerPID returns the PID that the kernel records for the socket's peer: the
// process that created the listening socket.
func peerPID(conn net.Conn) (int, error) {
	return socketOption(conn, func(fd int) (int, error) {
		cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			return 0, err
		}
		return int(cred.Pid), nil
	})
}
