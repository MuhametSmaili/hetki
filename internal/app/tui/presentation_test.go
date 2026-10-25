package tui

import (
	"os"
	"strings"
	"testing"

	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestPreparedPresentationResolvesFileThenExplicitOverrides(t *testing.T) {
	full := ui.Height{Value: 100, Percent: true}
	for _, tt := range []struct {
		name, file string
		overrides  Overrides
		height     ui.Height
		border     ui.BorderMode
		warning    string
		preview    bool
		width      int
	}{
		{name: "file rows", file: "ui: {height: 8, border: none, view: tree}", height: ui.Height{Value: 8}, border: ui.BorderNone},
		{name: "file percentage", file: "ui: {height: '80%', border: none, view: tree}", height: ui.Height{Value: 80, Percent: true}, border: ui.BorderNone},
		{name: "override only height", file: "ui: {height: 8, border: none, view: tree}", overrides: Overrides{Height: new(ui.Height{Value: 40, Percent: true})}, height: ui.Height{Value: 40, Percent: true}, border: ui.BorderNone},
		{name: "explicit default overrides inline file", file: "ui: {height: 8, border: none, view: tree}", overrides: Overrides{Height: &full, Border: new(ui.BorderRounded)}, height: full, border: ui.BorderRounded},
		{name: "percentage range falls back per field", file: "ui: {height: '101%', border: none, view: tree}", height: full, border: ui.BorderNone, warning: "configuration warning"},
		{name: "invalid choices fall back per field", file: "ui: {height: -1, border: wrong, view: tree}", height: full, border: ui.BorderRounded, warning: "configuration warning"},
		{name: "overflow falls back only height", file: "ui: {view: tree, height: 9223372036854775808, border: none}", height: full, border: ui.BorderNone, warning: "configuration warning"},
		{name: "extreme overflow falls back only height", file: "ui: {view: tree, height: 1" + strings.Repeat("0", 309) + ", border: none}", height: full, border: ui.BorderNone, warning: "configuration warning"},
		{name: "invalid string falls back only height", file: "ui: {view: tree, height: _12, border: none}", height: full, border: ui.BorderNone, warning: "configuration warning"},
		{name: "quoted rows are decimal", file: "ui: {view: tree, height: '08', border: none}", height: ui.Height{Value: 8}, border: ui.BorderNone},
		{name: "quoted fraction defaults only height", file: "ui: {view: tree, height: '1.5', border: none}", height: full, border: ui.BorderNone, warning: "configuration warning"},
		{name: "numeric fraction discards file", file: "ui: {view: tree, height: 1.5, border: none}", height: full, border: ui.BorderRounded, warning: "configuration ignored"},
		{name: "quoted hex defaults only height", file: "ui: {view: tree, height: '0x10', border: none}", height: full, border: ui.BorderNone, warning: "configuration warning"},
		{name: "numeric hex canonicalizes", file: "ui: {view: tree, height: 0x10, border: none}", height: ui.Height{Value: 16}, border: ui.BorderNone},
		{name: "quoted boolean defaults only height", file: "ui: {view: tree, height: 'true', border: none}", height: full, border: ui.BorderNone, warning: "configuration warning"},
		{name: "Boolean preview and width with file height", file: "ui: {view: tree, preview: false, preview_width: 45, height: '40%', border: none}", height: ui.Height{Value: 40, Percent: true}, border: ui.BorderNone, width: 45},
		{name: "flags preserve Boolean preview and width", file: "ui: {view: tree, preview: true, preview_width: 45, height: '40%', border: rounded}", overrides: Overrides{Height: &full, Border: new(ui.BorderNone)}, height: full, border: ui.BorderNone, preview: true, width: 45},
		{name: "bad width preserves height and Boolean preview", file: "ui: {view: tree, preview: true, preview_width: 0, height: '80%', border: none}", height: ui.Height{Value: 80, Percent: true}, border: ui.BorderNone, preview: true, warning: "configuration warning"},
		{name: "mistyped width discards file but keeps flags", file: "ui: {view: tree, preview: true, preview_width: '45%', height: '40%'}", overrides: Overrides{Height: new(ui.Height{Value: 12})}, height: ui.Height{Value: 12}, border: ui.BorderRounded, warning: "configuration ignored"},
		{name: "removed layout is structural", file: "ui: {view: tree, layout: inline, height: 12}", height: full, border: ui.BorderRounded, warning: "configuration ignored"},
		{name: "bad structure still allows overrides", file: "ui: {view: tree, height: null}", overrides: Overrides{Height: new(ui.Height{Value: 6}), Border: new(ui.BorderNone)}, height: ui.Height{Value: 6}, border: ui.BorderNone, warning: "configuration ignored"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := privateConfig(t, tt.file)

			service, err := prepareService(t.Context(), nil, tt.overrides)

			require.NoError(t, err)
			require.Equal(t, tt.height, service.Options.Height)
			require.Equal(t, tt.border, service.Options.Border)
			require.Equal(t, tt.preview, service.Options.Initial.Preview)
			width := tt.width
			if width == 0 {
				width = ui.DefaultOptions().PreviewWidth
			}
			require.Equal(t, width, service.Options.PreviewWidth)
			require.NoError(t, service.Options.Validate())
			if tt.warning == "configuration ignored" {
				require.Equal(t, ui.ViewFlat, service.Options.Initial.View)
			} else {
				require.Equal(t, ui.ViewTree, service.Options.Initial.View)
			}
			if tt.warning == "" {
				require.Empty(t, service.Notice)
			} else {
				require.Contains(t, service.Notice, tt.warning)
				require.NotContains(t, service.Notice, "wrong")
			}
			contents, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, tt.file, string(contents))
		})
	}
}

func TestInvalidOverridesFailBeforeStartup(t *testing.T) {
	for _, overrides := range []Overrides{
		{Height: new(ui.Height{})}, {Height: new(ui.Height{Value: -1})},
		{Height: new(ui.Height{Value: 101, Percent: true})}, {Border: new(ui.BorderMode("other"))},
	} {
		err := Open(t.Context(), func() (Backend, error) {
			t.Fatal("invalid invocation reached backend")
			return nil, nil
		}, overrides)

		require.Error(t, err)
	}
}
