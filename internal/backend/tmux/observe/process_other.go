//go:build !darwin && !linux

package observe

import (
	"context"
	"fmt"
)

func readProcess(ctx context.Context, _ int) (processInfo, error) {
	if err := ctx.Err(); err != nil {
		return processInfo{}, err
	}

	return processInfo{}, fmt.Errorf("native tmux status identity is unsupported on this platform")
}
