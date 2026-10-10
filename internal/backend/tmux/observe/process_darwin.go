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

func readProcess(ctx context.Context, pid int) (processInfo, error) {
	if err := ctx.Err(); err != nil {
		return processInfo{}, err
	}

	if pid <= 0 || pid > math.MaxInt32 {
		return processInfo{}, fmt.Errorf("invalid process PID")
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
		return processInfo{}, fmt.Errorf("read native process metadata: %w", errno)
	}

	if size != unsafe.Sizeof(proc) || proc.Proc.P_pid != int32(pid) || proc.Proc.P_flag&processExiting != 0 {
		return processInfo{}, fmt.Errorf("native process unavailable or exiting")
	}

	start := proc.Proc.P_starttime
	if start.Sec <= 0 || start.Usec < 0 || start.Usec >= 1_000_000 {
		return processInfo{}, fmt.Errorf("invalid native process birth")
	}

	info := processInfo{
		Process:    Process{PID: pid, Birth: fmt.Sprintf("%d:%d", start.Sec, start.Usec)},
		group:      int(proc.Eproc.Pgid),
		foreground: int(proc.Eproc.Tpgid),
	}
	if proc.Eproc.Tdev != -1 {
		info.terminal = uint64(uint32(proc.Eproc.Tdev)) + 1
	}
	return info, ctx.Err()
}
