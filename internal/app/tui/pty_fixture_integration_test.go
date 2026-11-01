//go:build integration

package tui

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type ptyStep struct {
	marker string
	keys   string
}

func ptyInput(input io.Writer, steps []ptyStep) func([]byte) error {
	stage := 0
	return func(all []byte) error {
		if stage == len(steps) || !bytes.Contains(all, []byte(steps[stage].marker)) {
			return nil
		}
		keys := steps[stage].keys
		stage++
		_, err := input.Write([]byte(keys))
		return err
	}
}

// Use the same writer for stdout and stderr so os/exec serializes callbacks.
// Cmd.Run joins its output copier before the test reads the buffer.
type ptyOutput struct {
	buffer  bytes.Buffer
	onWrite func([]byte) error
	err     error
}

func (o *ptyOutput) Write(p []byte) (int, error) {
	n, err := o.buffer.Write(p)
	if err == nil {
		err = o.onWrite(o.buffer.Bytes())
	}
	if err != nil && o.err == nil {
		o.err = err
	}
	return n, err
}

func ptyTerminalState(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "stty", "-a")
	cmd.Stdin = os.Stdin
	output, err := cmd.Output()
	require.NoError(t, err)
	// PENDIN is a transient kernel input-queue flag, not a terminal preference.
	fields := slices.DeleteFunc(strings.Fields(string(output)), func(field string) bool {
		flag := strings.TrimSuffix(field, ";")
		return flag == "pendin" || flag == "-pendin"
	})
	// Window dimensions deliberately change in the resize case.
	dimensions := regexp.MustCompile(`\b([0-9]+ (rows|columns)|(rows|columns) [0-9]+);?`)
	state := dimensions.ReplaceAllString(strings.Join(fields, " "), "")
	return strings.Join(strings.Fields(state), " ")
}
