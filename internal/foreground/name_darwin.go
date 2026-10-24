package foreground

import (
	"context"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Darwin's sysctl selectors and process flags from sys/sysctl.h and sys/proc.h.
const (
	kernProc      = 14
	kernProcPID   = 1
	kernProcArgs2 = 49
	processLP64   = 0x00000004
	processWExit  = 0x00002000
)

func processName(ctx context.Context, anchorPID int) string {
	root, ok := readDarwinProcess(anchorPID)
	if !ok || root.Eproc.Tpgid <= 0 || root.Eproc.Tdev == -1 || ctx.Err() != nil {
		return ""
	}
	pgid := int(root.Eproc.Tpgid)
	leader, ok := foregroundLeader(root, pgid)
	if !ok || ctx.Err() != nil {
		return ""
	}
	// tmux names the foreground group leader, so refine that same process.
	// Helpers sharing the group do not make the leader's hint ambiguous.
	pointerSize := 4
	if leader.Proc.P_flag&processLP64 != 0 {
		pointerSize = 8
	}
	name := darwinProgramName(ctx, pgid, pointerSize)
	if name == "" {
		return ""
	}
	rootAfter, ok := readDarwinProcess(anchorPID)
	if !ok || !sameDarwinProcess(root, rootAfter) || ctx.Err() != nil {
		return ""
	}
	leaderAfter, ok := foregroundLeader(rootAfter, pgid)
	if !ok || !sameDarwinProcess(leader, leaderAfter) || ctx.Err() != nil {
		return ""
	}
	return name
}

// foregroundLeader reads the process whose PID matches the foreground group
// ID. A group can outlive its leader, so the PID read must still identify a
// live member of the same foreground job on the same terminal.
func foregroundLeader(root unix.KinfoProc, pgid int) (unix.KinfoProc, bool) {
	leader, ok := readDarwinProcess(pgid)
	return leader, ok &&
		leader.Eproc.Pgid == int32(pgid) &&
		leader.Eproc.Tpgid == root.Eproc.Tpgid &&
		leader.Eproc.Tdev == root.Eproc.Tdev
}

func sameDarwinProcess(before, after unix.KinfoProc) bool {
	return before.Proc.P_pid == after.Proc.P_pid &&
		before.Proc.P_starttime == after.Proc.P_starttime &&
		before.Proc.P_comm == after.Proc.P_comm &&
		before.Proc.P_flag&processLP64 == after.Proc.P_flag&processLP64 &&
		before.Eproc.Pgid == after.Eproc.Pgid &&
		before.Eproc.Tpgid == after.Eproc.Tpgid &&
		before.Eproc.Tdev == after.Eproc.Tdev
}

func readDarwinProcess(pid int) (unix.KinfoProc, bool) {
	var proc unix.KinfoProc
	n, err := boundedSysctl([]int32{unix.CTL_KERN, kernProc, kernProcPID, int32(pid)}, unsafe.Pointer(&proc), unsafe.Sizeof(proc))
	return proc, err == nil && n == int(unsafe.Sizeof(proc)) && proc.Proc.P_pid == int32(pid) && proc.Proc.P_flag&processWExit == 0
}

func darwinProgramName(ctx context.Context, pid, pointerSize int) string {
	if ctx.Err() != nil {
		return ""
	}
	mib := []int32{unix.CTL_KERN, kernProcArgs2, int32(pid)}
	// Preflight avoids copying a known oversized argument/environment block.
	// No allocation based on process-controlled lengths and no retry loop.
	size, err := boundedSysctl(mib, nil, 0)
	if err != nil || size < 4 || size >= maxProgramBytes || ctx.Err() != nil {
		return ""
	}
	var buf [maxProgramBytes]byte
	n, err := boundedSysctl(mib, unsafe.Pointer(&buf[0]), uintptr(len(buf)))
	// XNU can return a truncated tail instead of ENOMEM for an undersized
	// PROCARGS2 buffer. A capacity-sized result is never accepted, including
	// growth between preflight and read. Smaller results contain the full block.
	if err != nil || n >= len(buf) || ctx.Err() != nil {
		return ""
	}
	return processProgramName(parseDarwinArgvZero(buf[:n], pointerSize))
}

func boundedSysctl(mib []int32, buffer unsafe.Pointer, capacity uintptr) (int, error) {
	n := capacity
	_, _, errno := syscall.Syscall6(unix.SYS___SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)), uintptr(buffer), uintptr(unsafe.Pointer(&n)), 0, 0)
	runtime.KeepAlive(mib)
	runtime.KeepAlive(buffer)
	if errno != 0 {
		return 0, errno
	}
	if buffer != nil && n > capacity {
		return 0, syscall.ENOMEM
	}
	return int(n), nil
}
