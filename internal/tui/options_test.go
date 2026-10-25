package tui

import (
	"testing"

	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestRunRejectsInvalidOptionsBeforeTerminal(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func(*Options)
		message string
	}{
		{"missing view", func(o *Options) { o.Initial.View = "" }, "invalid view"},
		{"unknown view", func(o *Options) { o.Initial.View = "other" }, "invalid view"},
		{"missing scope", func(o *Options) { o.Initial.Scope = "" }, "invalid scope"},
		{"unknown scope", func(o *Options) { o.Initial.Scope = "other" }, "invalid scope"},
		{"missing keys", func(o *Options) { o.Keys = KeyMap{} }, "keymap"},
		{"missing start mode", func(o *Options) { o.StartMode = "" }, "invalid start mode"},
		{"unknown start mode", func(o *Options) { o.StartMode = "other" }, "invalid start mode"},
		{"zero width", func(o *Options) { o.PreviewWidth = 0 }, "preview width"},
		{"negative width", func(o *Options) { o.PreviewWidth = -1 }, "preview width"},
		{"excessive width", func(o *Options) { o.PreviewWidth = 100 }, "preview width"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := DefaultOptions()
			test.change(&options)

			result, err := Run(t.Context(), list.Snapshot{}, options, Runtime{})

			require.ErrorContains(t, err, test.message)
			require.Equal(t, Result{}, result)
		})
	}
}

func TestDefaultOptionsPreservePickerDefaults(t *testing.T) {
	options := DefaultOptions()

	require.NoError(t, options.Validate())
	require.Equal(t, State{View: ViewFlat, Scope: ScopeAll, Preview: false}, options.Initial)
	require.Equal(t, StartModeFilter, options.StartMode)
	require.Equal(t, 70, options.PreviewWidth)
	require.Equal(t, DefaultKeyMap(), options.Keys)
}
