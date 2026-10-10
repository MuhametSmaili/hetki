package osc7501

import "strings"

// The framer checks the actual terminator length. This is the largest possible
// payload with the five-byte selector, two-byte introducer and one-byte BEL.
const maxPayloadBytes = 4096 - 5 - 2 - 1

type report struct {
	id    string
	state State
	clear bool
}

func parse(payload string) (report, Result) {
	if payload == "?" {
		return report{}, Query
	}

	for pair := range strings.SplitSeq(payload, ":") {
		key, value, _ := strings.Cut(pair, "=")
		if !withinLimits(strings.TrimSpace(key), strings.TrimSpace(value)) {
			return report{}, Rejected
		}
	}

	return parseReport(payload)
}

func parseReport(payload string) (report, Result) {
	var result report
	invalidID := false
	for pair := range strings.SplitSeq(payload, ":") {
		key, value, found := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if !found || !validKey(key) {
			continue
		}

		// A malformed explicit ID must never fall back to the root record.
		if key == "id" {
			invalidID = !validID(value)
		}

		if !validValue(value) {
			continue
		}

		if !result.acceptPair(key, value) {
			return report{}, Rejected
		}
	}

	if invalidID || result.state == "" && !result.clear {
		return report{}, Ignored
	}
	return result, Applied
}

func (r *report) acceptPair(key, value string) bool {
	switch key {
	case "id":
		r.id = value
	case "state":
		r.state = recognizedState(value)
		r.clear = value == "clear"
	case "msg":
		return safeText(value, 2048)
	case "title":
		return safeText(value, 192)
	}
	return true
}

func recognizedState(value string) State {
	switch value {
	case "idle":
		return Idle
	case "working":
		return Working
	case "blocked":
		return Blocked
	case "done":
		return Done
	case "error":
		return Error
	default:
		return ""
	}
}

func withinLimits(key, value string) bool {
	if len(key) > 16 {
		return false
	}

	switch key {
	case "app":
		return len(value) <= 32
	case "msg":
		return len(value) <= 2732
	case "title":
		return len(value) <= 256
	case "id":
		return withinIDLimits(value)
	default:
		return true
	}
}

func withinIDLimits(id string) bool {
	if len(id) > 128 {
		return false
	}

	levels := 0
	for segment := range strings.SplitSeq(id, "/") {
		levels++
		if len(segment) > 32 || levels > 8 {
			return false
		}
	}
	return true
}

func validKey(key string) bool {
	if key == "" {
		return false
	}

	for _, c := range key {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

func validValue(value string) bool {
	for _, c := range value {
		if !identifierChar(c) && c != ',' && c != '/' && c != '=' {
			return false
		}
	}
	return true
}

func validID(id string) bool {
	for segment := range strings.SplitSeq(id, "/") {
		if segment == "" {
			return false
		}

		for _, c := range segment {
			if !identifierChar(c) {
				return false
			}
		}
	}
	return true
}

func identifierChar(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '_' || c == '.' || c == '+' || c == '-'
}
