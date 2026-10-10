package observe

import (
	"fmt"
	"strconv"
	"strings"
)

func parseProcessBirth(value string, pid int) (string, error) {
	open, close := strings.IndexByte(value, '('), strings.LastIndexByte(value, ')')
	if open < 1 || close <= open {
		return "", fmt.Errorf("invalid native process metadata")
	}

	observed, err := strconv.Atoi(strings.TrimSpace(value[:open]))
	fields := strings.Fields(value[close+1:])
	if err != nil || observed != pid || len(fields) < 20 {
		return "", fmt.Errorf("invalid native process fields")
	}

	if len(fields[0]) != 1 || fields[0] == "Z" || fields[0] == "X" || fields[0] == "x" {
		return "", fmt.Errorf("native process unavailable or exiting")
	}

	birth, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || birth == 0 {
		return "", fmt.Errorf("invalid native process birth")
	}

	return fields[19], nil
}
