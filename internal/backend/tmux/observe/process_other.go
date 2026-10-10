//go:build !darwin && !linux

package observe

import (
	"context"
	"fmt"
)

func readProcessBirth(ctx context.Context, _ int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	return "", fmt.Errorf("native tmux status identity is unsupported on this platform")
}
