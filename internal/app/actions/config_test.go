package actions

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validRecipe = `version: 1
actions:
  join:
    label: Join pane
    scope: [pane]
    steps:
      - select: destination
        source: {builtin: tmux.windows}
      - run:
          argv: [tmux, join-pane, -s, "${context.subject.pane_id}", -t, "${steps.destination.value.pane_target}"]
`

func TestDecodeRecipes(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		valid bool
	}{
		{"valid chain", validRecipe, true},
		{"fixed JSON values", strings.Replace(validRecipe, "{builtin: tmux.windows}", `{items: [{id: target, label: Target, value: {nested: {enabled: true, entries: [null, "text", 1]}}}]}`, 1), true},
		{"sessions with session filter", strings.Replace(validRecipe, "{builtin: tmux.windows}", `{builtin: tmux.sessions, session: "$1"}`, 1), true},
		{"sessions with unsupported window filter", strings.Replace(validRecipe, "{builtin: tmux.windows}", `{builtin: tmux.sessions, window: "@999"}`, 1), false},
		{"windows with both filters", strings.Replace(validRecipe, "{builtin: tmux.windows}", `{builtin: tmux.windows, session: "$1", window: "@2"}`, 1), true},
		{"panes with both filters", strings.Replace(validRecipe, "{builtin: tmux.windows}", `{builtin: tmux.panes, session: "$1", window: "@2"}`, 1), true},
		{"unknown field", validRecipe + "typo: true\n", false},
		{"duplicate action", validRecipe + "  join: {}\n", false},
		{"multiple documents", validRecipe + "---\n{}", false},
		{"unsupported version", strings.Replace(validRecipe, "version: 1", "version: 2", 1), false},
		{"early mutation", strings.Replace(validRecipe, "- select: destination\n        source: {builtin: tmux.windows}", "- run: {argv: [true]}", 1), false},
		{"ambiguous source", strings.Replace(validRecipe, "{builtin: tmux.windows}", "{builtin: tmux.windows, items: []}", 1), false},
		{"unbounded command timeout", strings.Replace(validRecipe, "- run:", "- run:\n          timeout: 0s", 1), false},
		{"YAML alias", "version: 1\nactions: &a {copy: *a}", false},
		{"oversized file", strings.Repeat(" ", maxConfigBytes+1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := Decode(strings.NewReader(test.input))

			if !test.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "destination", catalog.Actions["join"].Steps[0].Select)
		})
	}
}

func TestProviderRecordValidation(t *testing.T) {
	for _, test := range []struct {
		name, input string
		valid       bool
	}{
		{"empty list", "", true},
		{"opaque value", "{\"id\":\"a\",\"label\":\"Same\",\"value\":{\"nested\":{\"n\":42}}}\n", true},
		{"duplicate ID", "{\"id\":\"a\",\"label\":\"Same\"}\n{\"id\":\"a\",\"label\":\"Other\"}\n", false},
		{"unknown field", "{\"id\":\"a\",\"label\":\"A\",\"run\":\"oops\"}", false},
		{"trailing object", "{\"id\":\"a\",\"label\":\"A\"} {}", false},
		{"trailing delimiter", "{\"id\":\"a\",\"label\":\"A\"}]", false},
		{"record limit", strings.Repeat("x", maxRecord+1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeItems([]byte(test.input))

			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestNumericValuesPreservePrecisionAcrossSources(t *testing.T) {
	for _, number := range []string{"18446744073709551615", "9223372036854775807", "-9223372036854775808", "9007199254740993", "0", "1.25"} {
		t.Run(number, func(t *testing.T) {
			source := fmt.Sprintf(`{items: [{id: target, label: Target, value: {limit: %s}}]}`, number)
			catalog, err := Decode(strings.NewReader(strings.Replace(validRecipe, "{builtin: tmux.windows}", source, 1)))
			require.NoError(t, err)
			provided, err := decodeItems([]byte(fmt.Sprintf(`{"id":"target","label":"Target","value":{"limit":%s}}`, number)))
			require.NoError(t, err)
			fixed := catalog.Actions["join"].Steps[0].Source.Items[0]

			for _, item := range []Item{fixed, provided[0]} {
				request := Request{Steps: map[string]Item{"target": item}}
				args, err := resolveArgs([]string{"program", "${steps.target.value.limit}"}, request)
				require.NoError(t, err)
				assert.Equal(t, []string{"program", number}, args)
				encoded, err := json.Marshal(request)
				require.NoError(t, err)
				assert.Contains(t, string(encoded), `"limit":`+number+"}")
			}
		})
	}
}

func TestFixedValuesMustBeJSONCompatible(t *testing.T) {
	for _, value := range []string{"{ratio: .inf}", "{ratio: -.inf}", "{nested: [.nan]}"} {
		t.Run(value, func(t *testing.T) {
			source := fmt.Sprintf(`{items: [{id: target, label: Target, value: %s}]}`, value)
			input := strings.Replace(validRecipe, "{builtin: tmux.windows}", source, 1)

			_, err := Decode(strings.NewReader(input))

			require.ErrorContains(t, err, `action "join": step 1: item "target"`)
			require.ErrorContains(t, err, "JSON-compatible")
		})
	}
}

func TestReferencesPreserveArgumentsAndRejectMissingValues(t *testing.T) {
	request := Request{Steps: map[string]Item{"target": {ID: "x", Value: map[string]any{"path": "a b; $(exit 1)", "zero": 0, "empty": "", "null": nil}}}}

	args, err := resolveArgs([]string{"program", "${steps.target.value.path}", "--number=${steps.target.value.zero}", "${steps.target.value.empty}"}, request)

	require.NoError(t, err)
	assert.Equal(t, []string{"program", "a b; $(exit 1)", "--number=0", ""}, args)
	for _, reference := range []string{"${steps.target.value.missing}", "${steps.target.value.null}", "${steps.target.value}", "${steps.target"} {
		_, err := resolveArgs([]string{"program", reference}, request)
		require.Error(t, err)
	}
}
