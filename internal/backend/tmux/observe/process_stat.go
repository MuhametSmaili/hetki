package observe

import (
	"fmt"
	"strconv"
	"strings"
)

func parseProcessStat(value string, pid int) (processInfo, error) {
	open, close := strings.IndexByte(value, '('), strings.LastIndexByte(value, ')')
	if open < 1 || close <= open {
		return processInfo{}, fmt.Errorf("invalid native process metadata")
	}

	observed, err := strconv.Atoi(strings.TrimSpace(value[:open]))
	fields := strings.Fields(value[close+1:])
	if err != nil || observed != pid || len(fields) < 20 {
		return processInfo{}, fmt.Errorf("invalid native process fields")
	}

	if len(fields[0]) != 1 || fields[0] == "Z" || fields[0] == "X" || fields[0] == "x" {
		return processInfo{}, fmt.Errorf("native process unavailable or exiting")
	}

	birth, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || birth == 0 {
		return processInfo{}, fmt.Errorf("invalid native process birth")
	}

	group, groupErr := strconv.ParseInt(fields[2], 10, 32)
	foreground, foregroundErr := strconv.ParseInt(fields[5], 10, 32)
	terminal, terminalErr := strconv.ParseInt(fields[4], 10, 32)
	if groupErr != nil || foregroundErr != nil || terminalErr != nil || group < 0 || foreground < -1 {
		return processInfo{}, fmt.Errorf("invalid native process terminal fields")
	}
	info := processInfo{
		Process:    Process{PID: pid, Birth: strings.Clone(fields[19])},
		group:      int(group),
		foreground: int(foreground),
	}
	if terminal != 0 {
		info.terminal = uint64(uint32(terminal)) + 1
	}
	return info, nil
}
