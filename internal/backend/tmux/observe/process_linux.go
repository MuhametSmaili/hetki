package observe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"syscall"
)

func readProcess(ctx context.Context, pid int) (info processInfo, err error) {
	if err := ctx.Err(); err != nil {
		return processInfo{}, err
	}

	if pid <= 0 || pid > math.MaxInt32 {
		return processInfo{}, fmt.Errorf("invalid process PID")
	}

	file, err := os.OpenFile(fmt.Sprintf("/proc/%d/stat", pid), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return processInfo{}, fmt.Errorf("open native process metadata: %w", err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return processInfo{}, fmt.Errorf("read native process metadata: %w", err)
	}

	if len(data) > 4096 {
		return processInfo{}, fmt.Errorf("native process metadata exceeds limit")
	}

	if err := ctx.Err(); err != nil {
		return processInfo{}, err
	}

	return parseProcessStat(string(data), pid)
}
