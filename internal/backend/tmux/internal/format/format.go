// Package format parses tmux's escaped query fields and object identifiers.
package format

import (
	"fmt"
	"strconv"
	"strings"
)

// Fields splits one #{q:...}-escaped row: a backslash always
// introduces one literal byte, so an unescaped '|' is unambiguous.
func Fields(line string, want int) ([]string, error) {
	fields := make([]string, 0, want)
	var value strings.Builder
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			if i+1 == len(line) {
				return nil, fmt.Errorf("dangling escape in row %q", line)
			}
			i++
			value.WriteByte(line[i])
		case '|':
			fields = append(fields, value.String())
			value.Reset()
		default:
			value.WriteByte(line[i])
		}
	}
	fields = append(fields, value.String())
	if len(fields) != want {
		return nil, fmt.Errorf("expected %d fields, found %d", want, len(fields))
	}
	return fields, nil
}

// ValidateObjectID requires a prefix followed by a canonical nonnegative integer.
func ValidateObjectID(kind, value string, prefix byte) error {
	if len(value) < 2 || value[0] != prefix {
		return fmt.Errorf("invalid %s ID %q", kind, value)
	}
	number, err := strconv.Atoi(value[1:])
	if err != nil || number < 0 || strconv.Itoa(number) != value[1:] {
		return fmt.Errorf("invalid %s ID %q", kind, value)
	}
	return nil
}
