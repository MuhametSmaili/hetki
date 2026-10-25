package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Height is the total picker size in rows or percent of the terminal height.
// Only 100% selects fullscreen; a row count always selects inline rendering.
type Height struct {
	Value   int
	Percent bool
}

var errInvalidHeight = errors.New("height must be positive decimal rows or an integer percentage from 1% to 100%")

// ParseHeight accepts decimal rows or an integer percentage without whitespace.
func ParseHeight(text string) (Height, error) {
	percent := strings.HasSuffix(text, "%")
	digits := strings.TrimSuffix(text, "%")
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return Height{}, errInvalidHeight
	}
	value, err := strconv.Atoi(digits)
	if err != nil {
		return Height{}, errInvalidHeight
	}
	height := Height{Value: value, Percent: percent}
	return height, height.Validate()
}

func (h Height) Validate() error {
	if h.Value <= 0 || (h.Percent && h.Value > 100) {
		return errInvalidHeight
	}
	return nil
}

func (h Height) Fullscreen() bool { return h.Percent && h.Value == 100 }

// BorderMode controls the outer picker frame, not modal dialogs.
type BorderMode string

const (
	BorderRounded BorderMode = "rounded"
	BorderNone    BorderMode = "none"
)

func (b BorderMode) Validate() error {
	if b != BorderRounded && b != BorderNone {
		return fmt.Errorf("invalid border mode %q", b)
	}
	return nil
}

func (m model) displayHeight() int {
	height := m.height
	if height <= 0 {
		height = 24
	}
	if !m.inline {
		return height
	}
	rows := m.pickerHeight.Value
	if m.pickerHeight.Percent {
		// Split before multiplication so even very large terminal dimensions
		// cannot overflow. Percent is validated in the range 1..99 here.
		rows = height/100*rows + height%100*rows/100
	}
	return min(max(1, rows), max(1, height-1))
}
