package actions

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArgumentExpansionBudgets(t *testing.T) {
	const reference = "${steps.target.value.text}"
	request := Request{Steps: map[string]Item{"target": {Value: map[string]any{"text": strings.Repeat("x", 16*1024)}}}}
	for _, test := range []struct {
		name  string
		args  []string
		valid bool
	}{
		{"at value limit", []string{"true", strings.Repeat(reference, 4)}, true},
		{"amplified value", []string{"true", strings.Repeat(reference, 1000)}, false},
		{"literal suffix exceeds limit", []string{"true", strings.Repeat(reference, 4) + "x"}, false},
		{"literal exceeds limit", []string{"true", strings.Repeat("x", 64*1024+1)}, false},
		{"limit counts bytes", []string{"true", strings.Repeat("é", 32*1024+1)}, false},
		{"at total limit", []string{"true", strings.Repeat(reference, 4), strings.Repeat(reference, 4), strings.Repeat(reference, 4), strings.Repeat("x", 64*1024-4)}, true},
		{"total exceeds limit", []string{"true", strings.Repeat(reference, 4), strings.Repeat(reference, 4), strings.Repeat(reference, 4), strings.Repeat(reference, 4)}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			args, err := resolveArgs(test.args, request)

			if test.valid {
				require.NoError(t, err)
				assert.Len(t, args, len(test.args))
			} else {
				require.ErrorContains(t, err, "exceeds")
				assert.Nil(t, args)
			}
		})
	}
}

func TestNativeFilterExpansionBudget(t *testing.T) {
	request := Request{Steps: map[string]Item{"target": {Value: map[string]any{"text": strings.Repeat("x", 16*1024)}}}}
	filter := strings.Repeat("${steps.target.value.text}", 1000)
	for _, test := range []struct {
		name   string
		source SourceSpec
	}{
		{"session", SourceSpec{Builtin: "tmux.panes", Session: filter}},
		{"window", SourceSpec{Builtin: "tmux.panes", Window: filter}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := (inventorySource{}).List(t.Context(), test.source, request)

			require.ErrorContains(t, err, "exceeds")
		})
	}
}
