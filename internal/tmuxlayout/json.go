package tmuxlayout

import (
	"encoding/json"
	"io"
	"strings"
)

var jsonDocumentFields = map[string]uint16{"V": 1, "L": 2}

var jsonCellFields = map[string]uint16{
	"t": 1 << 0, "w": 1 << 1, "h": 1 << 2,
	"x": 1 << 3, "y": 1 << 4, "c": 1 << 5,
	"I": 1 << 6, "i": 1 << 7, "l": 1 << 8, "a": 1 << 9,
}

// Check exact keys, ambiguity and nesting before decoding recursive cells.
func validJSON(text string) bool {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if !validJSONValue(decoder, 0) {
		return false
	}

	_, err := decoder.Token()
	return err == io.EOF
}

func validJSONValue(decoder *json.Decoder, depth int) bool {
	// A cell adds an object and a children array, beneath the document object.
	if depth > 2*maxDepth+2 {
		return false
	}

	token, err := decoder.Token()
	if err != nil || token == nil {
		return false
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return true
	}

	switch delimiter {
	case '{':
		return validJSONObject(decoder, depth)
	case '[':
		for decoder.More() {
			if !validJSONValue(decoder, depth+1) {
				return false
			}
		}
		return takeJSONDelimiter(decoder, ']')
	default:
		return false
	}
}

func validJSONObject(decoder *json.Decoder, depth int) bool {
	fields := jsonCellFields
	if depth == 0 {
		fields = jsonDocumentFields
	}

	var seen uint16
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		key, ok := token.(string)
		bit := fields[key]
		if !ok || bit == 0 || seen&bit != 0 {
			return false
		}
		seen |= bit

		if !validJSONValue(decoder, depth+1) {
			return false
		}
	}
	return takeJSONDelimiter(decoder, '}')
}

func takeJSONDelimiter(decoder *json.Decoder, want json.Delim) bool {
	token, err := decoder.Token()
	return err == nil && token == want
}
