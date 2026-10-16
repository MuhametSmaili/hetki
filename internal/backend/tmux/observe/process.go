package observe

import (
	"context"
	"fmt"
)

// Process identifies a native process incarnation. Its zero value is unavailable.
type Process struct {
	PID   int
	Birth string
}

func (p Process) Identity() string {
	if p.PID <= 0 || p.Birth == "" {
		return ""
	}
	return fmt.Sprintf("%d:%s", p.PID, p.Birth)
}

type processInfo struct {
	Process
	group      int
	foreground int
	terminal   uint64
}

func readProcessBirth(ctx context.Context, pid int) (string, error) {
	info, err := readProcess(ctx, pid)
	return info.Birth, err
}

type processResult struct {
	info processInfo
	err  error
}

// Each sample gets its own cache, including failures. No process facts survive
// into the next validation pass, where PID reuse or foreground handoff may occur.
type processSample struct {
	read  func(context.Context, int) (processInfo, error)
	cache map[int]processResult
}

func (s *processSample) lookup(ctx context.Context, pid int) (processInfo, error) {
	if result, ok := s.cache[pid]; ok {
		return result.info, result.err
	}
	info, err := s.read(ctx, pid)
	s.cache[pid] = processResult{info: info, err: err}
	return info, err
}

func (s *processSample) paneProcesses(ctx context.Context, pid int) (Process, Process, error) {
	root, err := s.lookup(ctx, pid)
	if err != nil {
		return Process{}, Process{}, fmt.Errorf("read pane root %d: %w", pid, err)
	}
	if root.terminal == 0 || root.foreground <= 0 {
		return root.Process, Process{}, fmt.Errorf("pane root %d has no foreground terminal group", pid)
	}

	leader, err := s.lookup(ctx, root.foreground)
	if err != nil {
		return root.Process, Process{}, fmt.Errorf("read foreground leader %d: %w", root.foreground, err)
	}
	if leader.group != root.foreground || leader.foreground != root.foreground || leader.terminal != root.terminal {
		return root.Process, Process{}, fmt.Errorf("pane foreground group or terminal changed")
	}
	return root.Process, leader.Process, nil
}
