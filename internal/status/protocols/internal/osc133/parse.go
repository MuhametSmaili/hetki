package osc133

import "bytes"

// Complete OSC size minus ESC ], "133;" and BEL. The framer enforces the actual
// terminator's size. The supported dialect has no retained parameter strings.
const maxPayloadBytes = 4096 - 2 - 4 - 1

func parse(payload []byte) (Kind, ExitStatus) {
	if len(payload) == 0 || len(payload) > maxPayloadBytes {
		return None, ExitStatus{}
	}

	marker, parameter, hasParameter := bytes.Cut(payload, []byte(";"))
	if len(marker) != 1 {
		return None, ExitStatus{}
	}

	switch marker[0] {
	case 'A':
		// cl=line is the supported prompt-layout option. Other dialect options
		// (notably secondary prompts and shell IDs) need their own semantics.
		if !hasParameter || bytes.Equal(parameter, []byte("cl=line")) {
			return Prompt, ExitStatus{}
		}
	case 'B':
		if !hasParameter {
			return Input, ExitStatus{}
		}
	case 'C':
		if !hasParameter {
			return Execute, ExitStatus{}
		}
	case 'D':
		if bytes.Contains(parameter, []byte(";")) {
			return None, ExitStatus{}
		}
		return Finish, parseExit(parameter)
	}
	return None, ExitStatus{}
}

func parseExit(value []byte) ExitStatus {
	if len(value) == 0 {
		return ExitStatus{}
	}

	code := 0
	for _, c := range value {
		if c < '0' || c > '9' {
			return ExitStatus{}
		}

		code = code*10 + int(c-'0')
		if code > 255 {
			return ExitStatus{}
		}
	}
	return ExitStatus{Code: uint8(code), Known: true}
}
