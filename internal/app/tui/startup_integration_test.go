//go:build integration

package tui

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MSmaili/hetki/internal/app/statusobserver"
	"github.com/MSmaili/hetki/internal/backend"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/MSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

// Mask the optional status capability to measure the same native row query
// without the status hookup. Both cases omit terminal rendering and frecency I/O.
type rowsOnlyBackend struct{ backend.Backend }

func BenchmarkNativePickerOpening(b *testing.B) {
	native, _, _ := pickerDaemonFixture(b)
	executable := filepath.Join(b.TempDir(), "hetki")
	build := exec.CommandContext(b.Context(), "go", "build", "-o", executable, "../../..")
	out, err := build.CombinedOutput()
	require.NoError(b, err, "%s", out)
	server, err := native.StatusServer(b.Context())
	require.NoError(b, err)
	_, err = statusobserver.EnsureDaemon(b.Context(), server, executable)
	require.NoError(b, err)

	for _, tc := range []struct {
		name    string
		backend backend.Backend
	}{
		{"rows_only", rowsOnlyBackend{native}},
		{"with_status", native},
	} {
		b.Run(tc.name, func(b *testing.B) {
			adapter := newLiveAdapter(func(...string) (backend.Backend, error) { return tc.backend, nil }, nil, nil)
			service := Service{Driver: adapter, RunUI: func(context.Context, list.Snapshot, ui.KeyMap, ui.StartMode, ui.DispatchFunc, ui.PreviewOptions, ui.StatusOptions) (ui.BackendTarget, error) {
				return "", nil
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
