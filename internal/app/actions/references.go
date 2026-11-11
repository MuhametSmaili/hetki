package actions

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	maxExpandedText = 64 * 1024
	maxExpandedArgs = 256 * 1024
)

func resolveArgs(args []string, request Request) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("command has no executable")
	}
	resolved := make([]string, len(args))
	remaining := maxExpandedArgs
	for i, arg := range args {
		value, err := resolveText(arg, request, min(maxExpandedText, remaining))
		if err != nil {
			return nil, fmt.Errorf("argument %d (total budget %d bytes): %w", i+1, maxExpandedArgs, err)
		}
		if strings.ContainsRune(value, 0) {
			return nil, fmt.Errorf("command argument contains NUL")
		}
		resolved[i] = value
		remaining -= len(value)
	}
	return resolved, nil
}

func resolveText(text string, request Request, limit int) (string, error) {
	var result strings.Builder
	for {
		start := strings.Index(text, "${")
		if start < 0 {
			if err := appendExpandedText(&result, text, limit); err != nil {
				return "", err
			}
			return result.String(), nil
		}
		if err := appendExpandedText(&result, text[:start], limit); err != nil {
			return "", err
		}
		text = text[start+2:]
		end := strings.IndexByte(text, '}')
		if end < 0 {
			return "", fmt.Errorf("unterminated action reference")
		}
		value, err := request.reference(text[:end])
		if err != nil {
			return "", err
		}
		if err := appendExpandedText(&result, value, limit); err != nil {
			return "", err
		}
		text = text[end+1:]
	}
}

func appendExpandedText(result *strings.Builder, text string, limit int) error {
	if len(text) > limit-result.Len() {
		return fmt.Errorf("expanded text exceeds %d bytes", limit)
	}
	result.WriteString(text)
	return nil
}

func (r Request) reference(path string) (string, error) {
	parts := strings.Split(path, ".")
	var value any
	switch {
	case path == "context.server.socket":
		if r.Context.Server != nil {
			value = r.Context.Server.Socket
		}
	case len(parts) == 3 && parts[0] == "context" && parts[1] == "subject":
		if r.Context.Subject != nil {
			value = r.Context.Subject.field(parts[2])
		}
	case len(parts) >= 3 && parts[0] == "steps":
		item, ok := r.Steps[parts[1]]
		if !ok {
			return "", fmt.Errorf("missing selection %q", parts[1])
		}
		value = itemField(item, parts[2:])
	default:
		return "", fmt.Errorf("unknown action reference %q", path)
	}
	return scalarReference(path, value)
}

func itemField(item Item, path []string) any {
	if len(path) == 1 {
		switch path[0] {
		case "id":
			return item.ID
		case "label":
			return item.Label
		}
	}
	if path[0] != "value" {
		return nil
	}
	var value any = item.Value
	for _, part := range path[1:] {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[part]
	}
	return value
}

func scalarReference(path string, value any) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case bool:
		return strconv.FormatBool(value), nil
	case json.Number:
		return value.String(), nil
	case int:
		return strconv.Itoa(value), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case uint64:
		return strconv.FormatUint(value, 10), nil
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("reference %q is missing, null or not scalar", path)
	}
}
