package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/stretchr/testify/require"
)

func TestReportCommandErrorSeparatesHintsWithoutTrustingTheirContents(t *testing.T) {
	cause := &os.PathError{Op: "open", Path: "workspace\nHint: forged\x1b]2;title\x07.yaml", Err: os.ErrPermission}
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{
			"structured hint",
			fmt.Errorf("loading: %w", appshared.WithHint(cause, "Use --file ./work.yaml.\nError: forged")),
			"Error: loading: open workspace\\nHint: forged.yaml: permission denied\nHint: Use --file ./work.yaml.\\nError: forged\n",
		},
		{
			"plain text is not a hint",
			fmt.Errorf("loading: %w", cause),
			"Error: loading: open workspace\\nHint: forged.yaml: permission denied\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := captureLoggerOutput(t, func() { reportCommandError(t.Context(), test.err) })

			require.Equal(t, test.want, output)
			require.ErrorIs(t, test.err, os.ErrPermission)
			var pathError *os.PathError
			require.ErrorAs(t, test.err, &pathError)
			require.Same(t, cause, pathError)
		})
	}
}

func TestReportCommandErrorSuppressesOnlyExpectedCancellation(t *testing.T) {
	for _, test := range []struct {
		name     string
		err      error
		canceled bool
		want     string
	}{
		{"wrapped cancellation", appshared.WithHint(fmt.Errorf("query: %w", context.Canceled), "Retry."), true, ""},
		{"joined cancellations", errors.Join(context.Canceled, fmt.Errorf("query: %w", context.Canceled)), true, ""},
		{"joined failure", fmt.Errorf("save: %w", errors.Join(context.Canceled, errors.New("write failed"))), true, "Error: save: context canceled\\nwrite failed\n"},
		{"unexpected cancellation", context.Canceled, false, "Error: context canceled\n"},
		{"deadline", context.DeadlineExceeded, true, "Error: context deadline exceeded\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.canceled {
				cancel()
			}

			output := captureLoggerOutput(t, func() { reportCommandError(ctx, test.err) })

			require.Equal(t, test.want, output)
		})
	}
}

func TestRootHelpDescribesSupportedFeatures(t *testing.T) {
	var output bytes.Buffer
	rootCmd.SetOut(&output)
	t.Cleanup(func() { rootCmd.SetOut(nil) })

	err := rootCmd.Help()

	require.NoError(t, err)
	require.Contains(t, output.String(), "tmux")
	require.Contains(t, output.String(), "interactive picker")
	require.Contains(t, output.String(), "hetki start work")
	require.Contains(t, output.String(), "hetki save --file ./work.yaml")
	require.Contains(t, output.String(), "-V, --version")
	require.NotContains(t, output.String(), "session ownership")
	require.NotContains(t, output.String(), "Templates")
}
