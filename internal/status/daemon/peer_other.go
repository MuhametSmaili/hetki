//go:build !darwin && !linux

package daemon

import (
	"errors"
	"net"
)

// Without a peer PID, an instance on another protocol is never signaled.
func peerPID(net.Conn) (int, error) {
	return 0, errors.ErrUnsupported
}
