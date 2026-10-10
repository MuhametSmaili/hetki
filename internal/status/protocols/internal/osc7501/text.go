package osc7501

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

func safeText(value string, maxDecoded int) bool {
	encoding := base64.StdEncoding.Strict()
	if !strings.Contains(value, "=") {
		encoding = base64.RawStdEncoding.Strict()
	}

	// Encoded limits were checked first. The largest allowed encoding can decode
	// to 2,049 bytes; reserve that scratch space before enforcing the decoded cap.
	var scratch [2049]byte
	n, err := encoding.Decode(scratch[:], []byte(value))
	if err != nil || n > maxDecoded || !utf8.Valid(scratch[:n]) {
		return false
	}

	for _, c := range string(scratch[:n]) {
		if c < 0x20 || c >= 0x7f && c <= 0x9f {
			return false
		}
	}
	return true
}
