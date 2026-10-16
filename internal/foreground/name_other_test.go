//go:build !darwin

package foreground_test

import (
	"context"
	"os"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/foreground"
	"github.com/stretchr/testify/require"
)

func TestNameDoesNotApproximateUnsupportedForegroundGroups(t *testing.T) {
	require.Empty(t, foreground.Name(context.Background(), os.Getpid()))
}
