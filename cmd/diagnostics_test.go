package cmd

import (
	"bytes"
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
			fmt.Errorf("loading: %w", appshared.WithHint(cause, "Use --path ./work.yaml.\nError: forged")),
			"Error: loading: open workspace\\nHint: forged.yaml: permission denied\nHint: Use --path ./work.yaml.\\nError: forged\n",
		},
		{
			"plain text is not a hint",
			fmt.Errorf("loading: %w", cause),
			"Error: loading: open workspace\\nHint: forged.yaml: permission denied\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := captureLoggerOutput(t, func() { reportCommandError(test.err) })

			require.Equal(t, test.want, output)
			require.ErrorIs(t, test.err, os.ErrPermission)
			var pathError *os.PathError
			require.ErrorAs(t, test.err, &pathError)
			require.Same(t, cause, pathError)
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
	require.Contains(t, output.String(), "--name or --path")
	require.NotContains(t, output.String(), "Templates")
}
