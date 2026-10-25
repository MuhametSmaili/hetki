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
		width       *int
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
		{name: "width only", input: "ui: {preview_width: 45}", width: ptr(45)},
		{name: "explicit zero width", input: "ui: {preview_width: 0}", width: ptr(0)},
		{name: "preview false only", input: "ui: {preview: false}", preview: ptr(false)},
		{name: "YAML uppercase boolean", input: "ui: {preview: TRUE}", preview: ptr(true)},
		{name: "YAML title case boolean", input: "ui: {preview: False}", preview: ptr(false)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Decode(strings.NewReader(tt.input))

			require.NoError(t, err)
			require.Equal(t, config.Config{UI: config.UI{View: tt.view, Scope: tt.scope, Preview: tt.preview, PreviewWidth: tt.width}}, got)
		})
	}
}

func TestDecodeIntegerWidth(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  int
	}{
		{"1", 1}, {"99", 99}, {"-1", -1}, {"100", 100},
		{"+45", 45}, {"0b101101", 45}, {"0o55", 45}, {"055", 45},
		{"0x2d", 45}, {"0X2D", 45}, {"4_5", 45}, {"-0o55", -45},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, err := config.Decode(strings.NewReader("ui: {preview_width: " + tt.input + "}"))

			require.NoError(t, err)
			require.Equal(t, ptr(tt.want), got.UI.PreviewWidth)
		})
	}
}

func TestDecodeOverflowWidthRetainsOtherFields(t *testing.T) {
	for _, input := range []string{
		"9223372036854775808", "18446744073709551616", "-9223372036854775809",
		strings.Repeat("9", 500), "+" + strings.Repeat("9", 500), "-" + strings.Repeat("9", 500),
		"0x" + strings.Repeat("f", 100), "0b" + strings.Repeat("1", 100),
		"0o" + strings.Repeat("7", 100), "0" + strings.Repeat("7", 100),
		"-0x" + strings.Repeat("f", 100), "0x" + strings.Repeat("ff_", 100),
	} {
		t.Run(input[:min(24, len(input))], func(t *testing.T) {
			got, err := config.Decode(strings.NewReader("ui: {view: tree, scope: tracked, preview: true, preview_width: " + input + "}"))

			require.NoError(t, err)
			require.NotNil(t, got.UI.PreviewWidth)
			require.True(t, *got.UI.PreviewWidth < 1 || *got.UI.PreviewWidth > 99)
			require.Equal(t, ptr("tree"), got.UI.View)
			require.Equal(t, ptr("tracked"), got.UI.Scope)
			require.Equal(t, ptr(true), got.UI.Preview)
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
		{"width quoted number", "ui: {preview_width: '45'}", "ui.preview_width"},
		{"width string", "ui: {preview_width: secret-value}", "ui.preview_width"},
		{"width percentage", "ui: {preview_width: 45%}", "ui.preview_width"},
		{"width boolean", "ui: {preview_width: true}", "ui.preview_width"},
		{"width float", "ui: {preview_width: 45.5}", "ui.preview_width"},
		{"width integral float", "ui: {preview_width: 45.0}", "ui.preview_width"},
		{"width exponent", "ui: {preview_width: 1e2}", "ui.preview_width"},
		{"width null", "ui: {preview_width: null}", "ui.preview_width"},
		{"width empty", "ui: {preview_width: }", "ui.preview_width"},
		{"width sequence", "ui: {preview_width: [45]}", "ui.preview_width"},
		{"width mapping", "ui: {preview_width: {}}", "ui.preview_width"},
		{"width explicit integer tag", "ui: {preview_width: !!int 45}", "ui.preview_width"},
		{"width explicit string tag", "ui: {preview_width: !!str 45}", "ui.preview_width"},
		{"width custom tag", "ui: {preview_width: !secret-value 45}", "ui.preview_width"},
		{"duplicate zero width", "ui: {preview_width: 0, preview_width: 45}", "ui.preview_width"},
		{"width malformed hex", "ui: {preview_width: 0xGG}", "ui.preview_width"},
		{"width malformed binary", "ui: {preview_width: 0b102}", "ui.preview_width"},
		{"width malformed octal", "ui: {preview_width: 0o78}", "ui.preview_width"},
		{"width leading underscore", "ui: {preview_width: _12}", "ui.preview_width"},
		{"width oversized leading underscore", "ui: {preview_width: _" + strings.Repeat("9", 500) + "}", "ui.preview_width"},
		{"width oversized invalid legacy octal", "ui: {preview_width: 0" + strings.Repeat("7", 100) + "8}", "ui.preview_width"},
		{"width oversized invalid octal", "ui: {preview_width: 0o" + strings.Repeat("7", 100) + "9}", "ui.preview_width"},
		{"width oversized invalid hex", "ui: {preview_width: 0x" + strings.Repeat("f", 100) + "g}", "ui.preview_width"},
		{"width oversized invalid binary", "ui: {preview_width: 0b" + strings.Repeat("1", 100) + "2}", "ui.preview_width"},
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
			prefix := "ui: {view: tree, scope: tracked, preview: true, preview_width: 45}\n#"
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
