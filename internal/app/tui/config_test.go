package tui

import (
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/config"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestResolveConfig(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		state       ui.State
	}{
		{"defaults", "", ui.State{View: ui.ViewFlat, Scope: ui.ScopeAll, Preview: false}},
		{"explicit defaults", "ui: {view: flat, scope: all, preview: false}", ui.State{View: ui.ViewFlat, Scope: ui.ScopeAll, Preview: false}},
		{"view only", "ui: {view: tree}", ui.State{View: ui.ViewTree, Scope: ui.ScopeAll, Preview: false}},
		{"scope only", "ui: {scope: tracked}", ui.State{View: ui.ViewFlat, Scope: ui.ScopeTracked, Preview: false}},
		{"preview only", "ui: {preview: true}", ui.State{View: ui.ViewFlat, Scope: ui.ScopeAll, Preview: true}},
		{"full", "ui: {view: tree, scope: tracked, preview: true}", ui.State{View: ui.ViewTree, Scope: ui.ScopeTracked, Preview: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			preferences, err := config.Decode(strings.NewReader(tt.input))
			require.NoError(t, err)
			want := ui.DefaultOptions()
			want.Initial = tt.state

			got, warning := resolveConfig(preferences)

			require.Empty(t, warning)
			require.Equal(t, want, got)
		})
	}
}

func TestResolveConfigFallsBackPerField(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		state       ui.State
		fields      []string
	}{
		{"view", "ui: {view: wrong, scope: tracked, preview: true}", ui.State{View: ui.ViewFlat, Scope: ui.ScopeTracked, Preview: true}, []string{"ui.view"}},
		{"scope", "ui: {view: tree, scope: '', preview: true}", ui.State{View: ui.ViewTree, Scope: ui.ScopeAll, Preview: true}, []string{"ui.scope"}},
		{"both string choices", "ui: {view: '', scope: wrong, preview: true}", ui.State{View: ui.ViewFlat, Scope: ui.ScopeAll, Preview: true}, []string{"ui.view", "ui.scope"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			preferences, err := config.Decode(strings.NewReader(tt.input))
			require.NoError(t, err)
			want := ui.DefaultOptions()
			want.Initial = tt.state

			got, warning := resolveConfig(preferences)

			require.Equal(t, want, got)
			for _, field := range tt.fields {
				require.Contains(t, warning, field)
			}
			require.NotContains(t, warning, "wrong")
		})
	}
}

func TestResolveConfigWarningDoesNotEchoValues(t *testing.T) {
	malicious := "secret\x1b[31m\n" + strings.Repeat("x", 65536-len("secret\x1b[31m\n"))
	preferences := config.Config{UI: config.UI{View: &malicious, Scope: &malicious}}

	got, warning := resolveConfig(preferences)

	require.Equal(t, ui.DefaultOptions(), got)
	text := warning
	require.Less(t, len(text), 512, "warning must be bounded")
	require.NotContains(t, text, "secret")
	require.NotContains(t, text, "\x1b")
	require.Equal(t, 2, strings.Count(text, "ui."))
	for _, choice := range []string{"flat", "tree", "all", "tracked", "default"} {
		require.Contains(t, text, choice)
	}
}
