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

func readProcessBirth(ctx context.Context, pid int) (birth string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if pid <= 0 || pid > math.MaxInt32 {
		return "", fmt.Errorf("invalid process PID")
	}

	file, err := os.OpenFile(fmt.Sprintf("/proc/%d/stat", pid), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("open native process metadata: %w", err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", fmt.Errorf("read native process metadata: %w", err)
	}

	if len(data) > 4096 {
		return "", fmt.Errorf("native process metadata exceeds limit")
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	return parseProcessBirth(string(data), pid)
}
