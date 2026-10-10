package screen

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MSmaili/hetki/internal/status"
)

const MaxScreenBytes = 256 << 10

var ErrInvalidScreen = errors.New("invalid screen text")

// Evaluate consumes plain rendered UTF-8 text, not terminal redraw bytes.
// No match or conflicting states returns Unknown without error. Invalid input
// returns Unknown with an error, without retaining or including text in the error.
func (e *Evaluator) Evaluate(text string) (status.State, error) {
	if e == nil || len(e.rules) == 0 {
		return status.Unknown, fmt.Errorf("screen evaluator is uninitialized")
	}

	if len(text) > MaxScreenBytes || !utf8.ValidString(text) || strings.ContainsFunc(text, invalidControl) {
		return status.Unknown, ErrInvalidScreen
	}

	text = footer(text, e.rows)
	state := status.Unknown
	for _, rule := range e.rules {
		if !rule.matches(text) {
			continue
		}
		if state != status.Unknown && state != rule.state {
			return status.Unknown, nil
		}
		state = rule.state
	}
	return state, nil
}

func invalidControl(r rune) bool {
	return unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r'
}

func footer(text string, rows int) string {
	text = strings.TrimRight(text, " \t\r\n")
	end := len(text)
	for range rows {
		end = strings.LastIndexByte(text[:end], '\n')
		if end < 0 {
			return text
		}
	}
	return text[end+1:]
}

func (r compiledRule) matches(text string) bool {
	return matchGroup(r.all, text, true) &&
		(len(r.any) == 0 || matchGroup(r.any, text, false)) &&
		!matchGroup(r.none, text, false)
}

// all selects conjunction (empty means true) or disjunction (empty means false).
func matchGroup(group []matcher, text string, all bool) bool {
	for _, m := range group {
		if m.matches(text) != all {
			return !all
		}
	}
	return all
}

func (m matcher) matches(text string) bool {
	if m.regex != nil {
		return m.regex.MatchString(text)
	}
	return strings.Contains(text, m.literal)
}
