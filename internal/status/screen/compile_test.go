package screen_test

import (
	"regexp/syntax"
	"strings"
	"testing"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/screen"
	"github.com/stretchr/testify/require"
)

func TestCompileRejectsInvalidRules(t *testing.T) {
	validRule := screen.Rule{State: status.Idle, All: []screen.Matcher{{Contains: "ready"}}}
	cases := []struct {
		name   string
		config screen.Config
	}{
		{"missing region", screen.Config{Rules: []screen.Rule{validRule}}},
		{"negative region", screen.Config{LastNonblankRows: -1, Rules: []screen.Rule{validRule}}},
		{"oversized region", screen.Config{LastNonblankRows: 101, Rules: []screen.Rule{validRule}}},
		{"missing rules", screen.Config{LastNonblankRows: 4}},
		{"too many rules", screen.Config{LastNonblankRows: 4, Rules: make([]screen.Rule, 17)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evaluator, err := screen.Compile(tc.config)

			require.Error(t, err)
			require.Nil(t, evaluator)
		})
	}
}

func TestCompileRequiresPositiveBoundedMatchers(t *testing.T) {
	cases := []struct {
		name string
		rule screen.Rule
	}{
		{"unknown state", screen.Rule{State: status.Unknown, All: []screen.Matcher{{Contains: "ready"}}}},
		{"withdrawal", screen.Rule{State: status.Withdrawn, All: []screen.Matcher{{Contains: "ready"}}}},
		{"outcome", screen.Rule{State: "done", All: []screen.Matcher{{Contains: "ready"}}}},
		{"negative only", screen.Rule{State: status.Idle, None: []screen.Matcher{{Contains: "working"}}}},
		{"too many matchers", screen.Rule{State: status.Idle, All: make([]screen.Matcher, 17)}},
		{"empty matcher", screen.Rule{State: status.Idle, All: []screen.Matcher{{}}}},
		{"both matchers", screen.Rule{State: status.Idle, All: []screen.Matcher{{Contains: "ready", Regex: "ready"}}}},
		{"oversized literal", screen.Rule{State: status.Idle, All: []screen.Matcher{{Contains: strings.Repeat("x", 1025)}}}},
		{"oversized regex", screen.Rule{State: status.Idle, All: []screen.Matcher{{Regex: strings.Repeat("x", 1025)}}}},
		{"empty matching regex", screen.Rule{State: status.Idle, All: []screen.Matcher{{Regex: ".*"}}}},
		{"invalid any", screen.Rule{State: status.Idle, Any: []screen.Matcher{{Regex: "["}}}},
		{"invalid none", screen.Rule{State: status.Idle, All: []screen.Matcher{{Contains: "ready"}}, None: []screen.Matcher{{}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := screen.Config{LastNonblankRows: 4, Rules: []screen.Rule{tc.rule}}

			evaluator, err := screen.Compile(config)

			require.Error(t, err)
			require.Nil(t, evaluator)
		})
	}
}

func TestCompilePreservesRegexError(t *testing.T) {
	config := screen.Config{LastNonblankRows: 4, Rules: []screen.Rule{{
		State: status.Idle, All: []screen.Matcher{{Regex: "["}},
	}}}

	_, err := screen.Compile(config)

	var syntaxErr *syntax.Error
	require.ErrorAs(t, err, &syntaxErr)
}

func TestCompileOwnsRules(t *testing.T) {
	config := fixtureRules()
	evaluator, err := screen.Compile(config)
	require.NoError(t, err)

	config.LastNonblankRows = 1
	config.Rules[0].All[0].Contains = "different"
	config.Rules[0].Any[0].Contains = "different"
	config.Rules[0].None[0].Contains = "yes"
	config.Rules[0].State = status.Idle
	config.Rules[3].All[0].Regex = "approval"
	clear(config.Rules)
	state, err := evaluator.Evaluate("approval choices yes\nfooter")

	require.NoError(t, err)
	require.Equal(t, status.Blocked, state.State)
}

func TestCompileAcceptsLimits(t *testing.T) {
	pattern := strings.Repeat("x", 1024)
	matchers := make([]screen.Matcher, 16)
	for i := range matchers {
		matchers[i] = screen.Matcher{Contains: pattern}
	}
	rules := make([]screen.Rule, 16)
	for i := range rules {
		rules[i] = screen.Rule{State: status.Working, All: matchers}
	}
	config := screen.Config{LastNonblankRows: 100, Rules: rules}

	evaluator, err := screen.Compile(config)
	require.NoError(t, err)
	state, err := evaluator.Evaluate(pattern)

	require.NoError(t, err)
	require.Equal(t, status.Working, state.State)
}
