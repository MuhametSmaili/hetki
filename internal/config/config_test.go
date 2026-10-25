package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDecodePresence(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		view, scope *string
		preview     *bool
	}{
		{name: "empty"},
		{name: "comment", input: "# preferences\n"},
		{name: "empty mapping", input: "{}"},
		{name: "empty ui", input: "ui: {}"},
		{name: "partial", input: "ui:\n  view: tree", view: ptr("tree")},
		{name: "arbitrary strings", input: "ui: {view: secret-value, scope: other}", view: ptr("secret-value"), scope: ptr("other")},
		{name: "empty string", input: "ui: {view: ''}", view: ptr("")},
		{name: "explicit defaults", input: "ui: {view: flat, scope: all, preview: false}", view: ptr("flat"), scope: ptr("all"), preview: ptr(false)},
		{name: "full", input: "ui: {view: tree, scope: tracked, preview: true}", view: ptr("tree"), scope: ptr("tracked"), preview: ptr(true)},
		{name: "preview false only", input: "ui: {preview: false}", preview: ptr(false)},
		{name: "YAML uppercase boolean", input: "ui: {preview: TRUE}", preview: ptr(true)},
		{name: "YAML title case boolean", input: "ui: {preview: False}", preview: ptr(false)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Decode(strings.NewReader(tt.input))

			require.NoError(t, err)
			require.Equal(t, config.Config{UI: config.UI{View: tt.view, Scope: tt.scope, Preview: tt.preview}}, got)
		})
	}
}

func TestDecodeRejectsInvalidConfiguration(t *testing.T) {
	for _, tt := range []struct{ name, input, field string }{
		{"null document", "null", "configuration"},
		{"empty explicit document", "---\n", "configuration"},
		{"null ui", "ui:", "ui"},
		{"null field", "ui: {view: null}", "ui.view"},
		{"boolean", "ui: {view: true}", "ui.view"},
		{"number", "ui: {scope: 12}", "ui.scope"},
		{"preview on", "ui: {preview: on}", "ui.preview"},
		{"preview off", "ui: {preview: off}", "ui.preview"},
		{"preview string true", "ui: {preview: 'true'}", "ui.preview"},
		{"preview string false", "ui: {preview: \"false\"}", "ui.preview"},
		{"preview arbitrary string", "ui: {preview: secret-value}", "ui.preview"},
		{"preview empty string", "ui: {preview: ''}", "ui.preview"},
		{"preview null", "ui: {preview: null}", "ui.preview"},
		{"preview empty", "ui: {preview: }", "ui.preview"},
		{"preview number", "ui: {preview: 1}", "ui.preview"},
		{"preview sequence", "ui: {preview: [true]}", "ui.preview"},
		{"preview mapping", "ui: {preview: {}}", "ui.preview"},
		{"preview explicit bool tag", "ui: {preview: !!bool true}", "ui.preview"},
		{"preview explicit string tag", "ui: {preview: !!str false}", "ui.preview"},
		{"preview custom tag", "ui: {preview: !secret-value true}", "ui.preview"},
		{"duplicate preview false", "ui: {preview: false, preview: true}", "ui.preview"},
		{"explicit string tag", "ui: {view: !!str tree}", "ui.view"},
		{"explicit mapping tag", "!!map {}", "configuration"},
		{"custom root", "!secret-value {}", "configuration"},
		{"custom ui", "ui: !secret-value {}", "ui"},
		{"custom field", "ui: {view: !secret-value tree}", "ui.view"},
		{"custom key", "!secret-value ui: {}", "configuration"},
		{"nonstring key", "true: {}", "configuration"},
		{"complex key", "? [ui]\n: {}", "configuration"},
		{"duplicate root", "ui: {}\nui: {}", "ui"},
		{"duplicate field", "ui: {view: tree, view: flat}", "ui.view"},
		{"unknown root", "secret-value: {}", "configuration"},
		{"unknown field", "ui: {secret-value: true}", "ui"},
		{"remember not supported", "ui: {remember_state: true}", "ui"},
		{"second document", "{}\n---\n{}", "configuration"},
		{"empty second document", "{}\n---", "configuration"},
		{"trailing junk", "{}\nsecret-value", "configuration"},
		{"alias", "ui: {view: &v tree, scope: *v}", "configuration"},
		{"merge", "ui: {<<: {view: tree}}", "ui"},
		{"deep", "ui: " + strings.Repeat("[", 17) + "0" + strings.Repeat("]", 17), "configuration"},
		{"syntax", "ui: [secret-value", "configuration"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Decode(strings.NewReader(tt.input))

			require.Error(t, err)
			require.Contains(t, err.Error(), tt.field)
			require.NotContains(t, err.Error(), "secret-value")
			require.Equal(t, config.Config{}, got)
		})
	}
}

func TestDecodeByteLimit(t *testing.T) {
	for _, size := range []int{65535, 65536, 65537} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			prefix := "ui: {view: tree, scope: tracked, preview: true}\n#"
			input := strings.NewReader(prefix + strings.Repeat("x", size-len(prefix)))

			_, err := config.Decode(input)

			if size <= 65536 {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "64 KiB")
			}
		})
	}
}

func ptr[T any](value T) *T { return &value }
