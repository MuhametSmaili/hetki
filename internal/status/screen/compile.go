// Package screen evaluates bounded plain-text captures against application rules.
// It owns no acquisition, scheduling, profile loading or retained screen text.
package screen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/MSmaili/hetki/internal/status"
)

const (
	maxRegionRows   = 100
	maxRules        = 16
	maxMatchers     = 16
	maxPatternBytes = 1024
)

// Config selects the final rows after trimming trailing spaces, tabs, CR and LF.
// Internal blank rows count toward LastNonblankRows. Rules have no priority:
// matching different states produces unknown.
type Config struct {
	LastNonblankRows int
	Rules            []Rule
}

// Rule requires every All matcher, at least one Any matcher when present, and
// no None matcher. At least one positive (All or Any) matcher is required.
type Rule struct {
	State status.State
	All   []Matcher
	Any   []Matcher
	None  []Matcher
}

// Matcher specifies exactly one nonempty literal or Go regular expression.
type Matcher struct {
	Contains string
	Regex    string
}

// Evaluator owns immutable compiled rules and may be shared across panes.
// Its zero value is uninitialized; construct it with Compile.
type Evaluator struct {
	rows  int
	rules []compiledRule
}

type compiledRule struct {
	state status.State
	all   []matcher
	any   []matcher
	none  []matcher
}

type matcher struct {
	literal string
	regex   *regexp.Regexp
}

// Compile validates and copies the configuration, compiling regexes once.
// Later changes to config do not affect the evaluator.
func Compile(config Config) (*Evaluator, error) {
	if config.LastNonblankRows < 1 || config.LastNonblankRows > maxRegionRows {
		return nil, fmt.Errorf("screen region requires 1–%d last nonblank rows", maxRegionRows)
	}

	if len(config.Rules) == 0 || len(config.Rules) > maxRules {
		return nil, fmt.Errorf("screen requires 1–%d rules", maxRules)
	}

	evaluator := &Evaluator{rows: config.LastNonblankRows, rules: make([]compiledRule, len(config.Rules))}
	for i, rule := range config.Rules {
		compiled, err := compileRule(rule)
		if err != nil {
			return nil, fmt.Errorf("screen rule %d: %w", i+1, err)
		}
		evaluator.rules[i] = compiled
	}
	return evaluator, nil
}

func compileRule(rule Rule) (compiledRule, error) {
	switch rule.State {
	case status.Working, status.Blocked, status.Idle:
	default:
		return compiledRule{}, fmt.Errorf("unsupported activity %q", rule.State)
	}

	if len(rule.All)+len(rule.Any) == 0 || len(rule.All)+len(rule.Any)+len(rule.None) > maxMatchers {
		return compiledRule{}, fmt.Errorf("requires positive matchers and at most %d total matchers", maxMatchers)
	}

	compiled := compiledRule{state: status.State(strings.Clone(string(rule.State)))}
	var err error
	compiled.all, err = compileMatchers(rule.All)
	if err != nil {
		return compiledRule{}, fmt.Errorf("all: %w", err)
	}

	compiled.any, err = compileMatchers(rule.Any)
	if err != nil {
		return compiledRule{}, fmt.Errorf("any: %w", err)
	}

	compiled.none, err = compileMatchers(rule.None)
	if err != nil {
		return compiledRule{}, fmt.Errorf("none: %w", err)
	}
	return compiled, nil
}

func compileMatchers(specs []Matcher) ([]matcher, error) {
	compiled := make([]matcher, len(specs))
	for i, spec := range specs {
		m, err := compileMatcher(spec)
		if err != nil {
			return nil, fmt.Errorf("matcher %d: %w", i+1, err)
		}
		compiled[i] = m
	}
	return compiled, nil
}

func compileMatcher(spec Matcher) (matcher, error) {
	if (spec.Contains == "") == (spec.Regex == "") || len(spec.Contains)+len(spec.Regex) > maxPatternBytes {
		return matcher{}, fmt.Errorf("requires exactly one nonempty contains or regex, up to %d bytes", maxPatternBytes)
	}

	if spec.Contains != "" {
		return matcher{literal: strings.Clone(spec.Contains)}, nil
	}

	re, err := regexp.Compile(strings.Clone(spec.Regex))
	if err != nil {
		return matcher{}, fmt.Errorf("compile regex: %w", err)
	}

	if re.MatchString("") {
		return matcher{}, fmt.Errorf("regex must not match an empty screen")
	}
	return matcher{regex: re}, nil
}
