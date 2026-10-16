package foreground_test

import (
	"context"
	"math"
	"os"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/foreground"
	"github.com/stretchr/testify/require"
)

func TestNameFallsBackForInvalidOrUnavailableAnchor(t *testing.T) {
	for _, pid := range []int{0, -1, 1_000_000_000, math.MaxInt} {
		require.Empty(t, foreground.Name(context.Background(), pid))
	}
}

func TestNameFallsBackForCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Empty(t, foreground.Name(ctx, os.Getpid()))
}
