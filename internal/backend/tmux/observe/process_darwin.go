package observe

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func readProcessBirth(ctx context.Context, pid int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if pid <= 0 || pid > math.MaxInt32 {
		return "", fmt.Errorf("invalid process PID")
	}

	const kernProc, kernProcPID, processExiting = 14, 1, 0x00002000
	var proc unix.KinfoProc
	mib := []int32{unix.CTL_KERN, kernProc, kernProcPID, int32(pid)}
	size := unsafe.Sizeof(proc)
	_, _, errno := syscall.Syscall6(unix.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)),
		uintptr(unsafe.Pointer(&proc)), uintptr(unsafe.Pointer(&size)), 0, 0)
	runtime.KeepAlive(mib)
	runtime.KeepAlive(&proc)
	if errno != 0 {
		return "", fmt.Errorf("read native process metadata: %w", errno)
	}

	if size != unsafe.Sizeof(proc) || proc.Proc.P_pid != int32(pid) || proc.Proc.P_flag&processExiting != 0 {
		return "", fmt.Errorf("native process unavailable or exiting")
	}

	start := proc.Proc.P_starttime
	if start.Sec <= 0 || start.Usec < 0 || start.Usec >= 1_000_000 {
		return "", fmt.Errorf("invalid native process birth")
	}

	return fmt.Sprintf("%d:%d", start.Sec, start.Usec), ctx.Err()
}
