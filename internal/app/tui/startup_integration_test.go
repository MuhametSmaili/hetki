//go:build integration

package tui

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/app/statusobserver"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

// Mask the optional status capability to measure the same native row query
// without the status hookup. Both cases omit terminal rendering and frecency I/O.
type rowsOnlyBackend struct{ Backend }

func BenchmarkNativePickerOpening(b *testing.B) {
	executable := filepath.Join(b.TempDir(), "hetki")
	build := exec.CommandContext(b.Context(), "go", "build", "-o", executable, "../../..")
	out, err := build.CombinedOutput()
	require.NoError(b, err, "%s", out)
	native, _, _ := pickerDaemonFixture(b)
	server, err := native.StatusServer(b.Context())
	require.NoError(b, err)
	_, err = statusobserver.EnsureDaemon(b.Context(), server, executable)
	require.NoError(b, err)

	for _, tc := range []struct {
		name    string
		backend Backend
	}{
		{"rows_only", rowsOnlyBackend{native}},
		{"with_status", native},
	} {
		b.Run(tc.name, func(b *testing.B) {
			adapter := newLiveAdapter(func() (Backend, error) { return tc.backend, nil }, nil, nil)
			service := Service{Options: ui.DefaultOptions(), Driver: adapter, RunUI: func(context.Context, list.Snapshot, ui.Options, ui.Runtime) (ui.Result, error) {
				return ui.Result{}, nil
			}}
			b.ReportAllocs()
			for b.Loop() {
				if err := service.Run(b.Context()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
