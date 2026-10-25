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
		{name: "preview flag enables configured false", file: "ui: {view: tree, preview: false, preview_width: 45, height: '40%', border: none}", overrides: Overrides{Preview: new(true)}, height: ui.Height{Value: 40, Percent: true}, border: ui.BorderNone, preview: true, width: 45},
		{name: "preview flag disables configured true", file: "ui: {view: tree, preview: true, preview_width: 45, height: '40%', border: none}", overrides: Overrides{Preview: new(false)}, height: ui.Height{Value: 40, Percent: true}, border: ui.BorderNone, width: 45},
		{name: "preview combines with presentation flags", file: "ui: {view: tree, preview: true, preview_width: 45, height: '40%', border: rounded}", overrides: Overrides{Height: new(ui.Height{Value: 12}), Border: new(ui.BorderNone), Preview: new(false)}, height: ui.Height{Value: 12}, border: ui.BorderNone, width: 45},
		{name: "bad configuration keeps preview flag", file: "ui: {view: tree, preview: 'wrong'}", overrides: Overrides{Preview: new(true)}, height: full, border: ui.BorderRounded, preview: true, warning: "configuration ignored"},
		{name: "width flag preserves disabled preview", file: "ui: {view: tree, preview: false, preview_width: 45, height: '40%', border: none}", overrides: Overrides{PreviewWidth: new(65)}, height: ui.Height{Value: 40, Percent: true}, border: ui.BorderNone, width: 65},
		{name: "minimum width flag", file: "ui: {view: tree, preview: true, preview_width: 45}", overrides: Overrides{PreviewWidth: new(1)}, height: full, border: ui.BorderRounded, preview: true, width: 1},
		{name: "maximum width flag", file: "ui: {view: tree, preview: true, preview_width: 45}", overrides: Overrides{PreviewWidth: new(99)}, height: full, border: ui.BorderRounded, preview: true, width: 99},
		{name: "bad width keeps explicit width flag", file: "ui: {view: tree, preview: true, preview_width: 0, height: '80%', border: none}", overrides: Overrides{PreviewWidth: new(65)}, height: ui.Height{Value: 80, Percent: true}, border: ui.BorderNone, preview: true, width: 65, warning: "configuration warning"},
		{name: "bad structure keeps width flag", file: "ui: {view: tree, preview_width: 'wrong'}", overrides: Overrides{PreviewWidth: new(65)}, height: full, border: ui.BorderRounded, width: 65, warning: "configuration ignored"},
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

func TestPreparedScopeResolvesFileThenExplicitOverrides(t *testing.T) {
	for _, tt := range []struct {
		name, file string
		scope      *ui.Scope
		want       ui.Scope
		warning    string
	}{
		{name: "default", want: ui.ScopeAll},
		{name: "configured tracked", file: "ui: {scope: tracked}", want: ui.ScopeTracked},
		{name: "explicit tracked overrides all", file: "ui: {scope: all}", scope: new(ui.ScopeTracked), want: ui.ScopeTracked},
		{name: "explicit all overrides tracked", file: "ui: {scope: tracked}", scope: new(ui.ScopeAll), want: ui.ScopeAll},
		{name: "invalid file choice falls back", file: "ui: {scope: working}", want: ui.ScopeAll, warning: "configuration warning"},
		{name: "invalid file choice keeps explicit scope", file: "ui: {scope: working}", scope: new(ui.ScopeTracked), want: ui.ScopeTracked, warning: "configuration warning"},
		{name: "invalid file structure keeps explicit scope", file: "ui: {scope: null}", scope: new(ui.ScopeTracked), want: ui.ScopeTracked, warning: "configuration ignored"},
		{name: "unsupported filter key keeps explicit scope", file: "ui: {filter: tracked}", scope: new(ui.ScopeTracked), want: ui.ScopeTracked, warning: "configuration ignored"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := privateConfig(t, tt.file)
			overrides := Overrides{Scope: tt.scope, Preview: new(true), Height: new(ui.Height{Value: 12}), Border: new(ui.BorderNone)}

			service, err := prepareService(t.Context(), nil, overrides)

			require.NoError(t, err)
			require.Equal(t, tt.want, service.Options.Initial.Scope)
			require.True(t, service.Options.Initial.Preview)
			require.Equal(t, ui.Height{Value: 12}, service.Options.Height)
			require.Equal(t, ui.BorderNone, service.Options.Border)
			require.NoError(t, service.Options.Validate())
			if tt.warning == "" {
				require.Empty(t, service.Notice)
			} else {
				require.Contains(t, service.Notice, tt.warning)
			}
			contents, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, tt.file, string(contents))
		})
	}
}

func TestPreparedViewResolvesFileThenExplicitOverrides(t *testing.T) {
	for _, tt := range []struct {
		name, file string
		view       *ui.ViewMode
		want       ui.ViewMode
		warning    bool
	}{
		{name: "default", want: ui.ViewFlat},
		{name: "configured tree", file: "ui: {view: tree}", want: ui.ViewTree},
		{name: "explicit flat overrides tree", file: "ui: {view: tree}", view: new(ui.ViewFlat), want: ui.ViewFlat},
		{name: "explicit tree overrides flat", file: "ui: {view: flat}", view: new(ui.ViewTree), want: ui.ViewTree},
		{name: "invalid file choice keeps explicit view", file: "ui: {view: wrong}", view: new(ui.ViewTree), want: ui.ViewTree, warning: true},
		{name: "invalid file structure keeps explicit view", file: "ui: {view: null}", view: new(ui.ViewTree), want: ui.ViewTree, warning: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := privateConfig(t, tt.file)
			overrides := Overrides{View: tt.view, Scope: new(ui.ScopeTracked), Preview: new(true)}

			service, err := prepareService(t.Context(), nil, overrides)

			require.NoError(t, err)
			require.Equal(t, tt.want, service.Options.Initial.View)
			require.Equal(t, ui.ScopeTracked, service.Options.Initial.Scope)
			require.True(t, service.Options.Initial.Preview)
			require.NoError(t, service.Options.Validate())
			if tt.warning {
				require.Contains(t, service.Notice, "configuration")
			} else {
				require.Empty(t, service.Notice)
			}
			contents, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, tt.file, string(contents))
		})
	}
}

func TestInvalidPreferencesDoNotClaimDefaultsOverExplicitFlags(t *testing.T) {
	privateConfig(t, "ui: {view: wrong, scope: wrong, preview_width: 0, preview: true, height: 12, border: none}")
	overrides := Overrides{View: new(ui.ViewTree), Scope: new(ui.ScopeTracked), PreviewWidth: new(45)}

	service, err := prepareService(t.Context(), nil, overrides)

	require.NoError(t, err)
	require.Equal(t, ui.State{View: ui.ViewTree, Scope: ui.ScopeTracked, Preview: true}, service.Options.Initial)
	require.Equal(t, 45, service.Options.PreviewWidth)
	require.Equal(t, ui.Height{Value: 12}, service.Options.Height)
	require.Equal(t, ui.BorderNone, service.Options.Border)
	for _, field := range []string{"ui.view", "ui.scope", "ui.preview_width"} {
		require.Contains(t, service.Notice, field)
	}
	require.NotContains(t, service.Notice, "using default")
	require.NotContains(t, service.Notice, "wrong")
}

func TestInvalidOverridesFailBeforeStartup(t *testing.T) {
	for _, overrides := range []Overrides{
		{Height: new(ui.Height{})}, {Height: new(ui.Height{Value: -1})},
		{Height: new(ui.Height{Value: 101, Percent: true})}, {Border: new(ui.BorderMode("other"))},
		{Scope: new(ui.Scope(""))}, {Scope: new(ui.Scope("working"))},
		{View: new(ui.ViewMode(""))}, {View: new(ui.ViewMode("other"))},
		{PreviewWidth: new(0)}, {PreviewWidth: new(-1)}, {PreviewWidth: new(100)},
	} {
		err := Open(t.Context(), func() (Backend, error) {
			t.Fatal("invalid invocation reached backend")
			return nil, nil
		}, overrides)

		require.Error(t, err)
	}
}
