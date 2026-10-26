// Package terminalstream frames bounded seven-bit OSC sequences and full resets
// from incremental terminal output. It does not interpret protocol payloads or
// emulate a screen. Eight-bit C1 bytes are not recognized as control sequences.
package terminalstream

// MaxSequenceBytes includes the OSC introducer and its BEL or ST terminator.
const MaxSequenceBytes = 4096

// Kind identifies the terminal operation delivered to an observer.
type Kind uint8

const (
	OSC Kind = iota + 1
	RIS
)

// Frame contains one complete operation. Body excludes OSC framing; it is empty
// for RIS. Body is read-only and borrowed until the callback returns.
type Frame struct {
	Kind Kind
	Body []byte
}

type state uint8

const (
	ground state = iota
	escape
	escapeIntermediate
	csi
	osc
	oscEscape
	dcs
	dcsEscape
	controlString
)

// Decoder belongs to one uninterrupted pane stream. Its zero value is ready to
// use. Calls must be serialized; callbacks must not reenter or mutate Decoder.
// Do not copy a Decoder after first use.
// Only OSC bodies are buffered, lazily, up to MaxSequenceBytes-3 bytes. Other
// strings (DCS, SOS, PM and APC) are skipped without retaining their contents,
// matching tmux: in DCS, ESC pairs with the next byte, so wrapped passthrough
// sequences stay opaque; SOS, PM and APC end at any ESC, which starts the next
// sequence.
type Decoder struct {
	state  state
	length int
	body   []byte
}

// Reset discards partial input and releases its buffer without emitting RIS.
// Call it whenever stream continuity is lost, before accepting replacement bytes.
func (d *Decoder) Reset() {
	*d = Decoder{}
}

// Feed consumes borrowed bytes and synchronously emits complete OSC/RIS frames
// in order. Emit must be nonnil. Aborted and oversized OSCs emit nothing; no
// partial frame is completed at a chunk boundary or by Reset.
func (d *Decoder) Feed(data []byte, emit func(Frame)) {
	for _, c := range data {
		d.advance(c, emit)
	}
}

func (d *Decoder) advance(c byte, emit func(Frame)) {
	if c == 0x18 || c == 0x1a { // CAN and SUB abort every unfinished sequence.
		d.discard()
		return
	}

	switch d.state {
	case ground:
		if c == 0x1b {
			d.beginEscape()
		}
	case escape, escapeIntermediate:
		d.advanceEscape(c, emit)
	case osc, oscEscape:
		d.advanceOSC(c, emit)
	case dcs, dcsEscape:
		d.skipDCS(c)
	case csi:
		d.skipCSI(c)
	case controlString:
		d.skipControlString(c)
	}
}

func (d *Decoder) beginEscape() {
	d.state = escape
	d.length = 1
	d.body = d.body[:0]
}

func (d *Decoder) discard() {
	d.state = ground
	d.length = 0
	d.body = d.body[:0]
}

func (d *Decoder) countByte() {
	// Saturate so even an indefinitely unterminated stream cannot overflow.
	if d.length <= MaxSequenceBytes {
		d.length++
	}
}

func (d *Decoder) advanceEscape(c byte, emit func(Frame)) {
	if c == 0x1b {
		d.beginEscape()
		return
	}

	d.countByte()
	if c < 0x20 || c == 0x7f {
		return
	}

	if c >= 0x20 && c <= 0x2f {
		d.state = escapeIntermediate
		return
	}

	if d.state == escapeIntermediate || c >= 0x80 {
		d.discard()
		return
	}

	d.dispatchEscape(c, emit)
}

func (d *Decoder) dispatchEscape(c byte, emit func(Frame)) {
	switch c {
	case ']':
		d.state = osc
	case '[':
		d.state = csi
	case 'P':
		d.state = dcs
	case 'X', '^', '_':
		d.state = controlString
	case 'c':
		d.discard()
		emit(Frame{Kind: RIS})
	default:
		d.discard()
	}
}

func (d *Decoder) skipCSI(c byte) {
	if c == 0x1b {
		d.beginEscape()
		return
	}

	if c >= 0x40 && c <= 0x7e {
		d.discard()
	}
}

// skipControlString skips SOS, PM and APC. Any ESC ends the string and starts
// the next sequence, so ESC \\ (ST) ends as an ordinary escape.
func (d *Decoder) skipControlString(c byte) {
	if c == 0x1b {
		d.beginEscape()
	}
}

func (d *Decoder) skipDCS(c byte) {
	if d.state == dcsEscape {
		d.state = dcs
		if c == '\\' {
			d.discard()
		}
		return
	}

	if c == 0x1b {
		d.state = dcsEscape
	}
}

func (d *Decoder) advanceOSC(c byte, emit func(Frame)) {
	d.countByte()
	if d.state == oscEscape {
		if c == '\\' {
			d.finishOSC(emit)
			return
		}

		// An ESC that does not form ST abandons the OSC and starts a new escape.
		d.beginEscape()
		d.advance(c, emit)
		return
	}

	switch c {
	case 0x07:
		d.finishOSC(emit)
	case 0x1b:
		d.state = oscEscape
	default:
		d.appendBody(c)
	}
}

func (d *Decoder) appendBody(c byte) {
	// Leave room for at least BEL; a two-byte ST is checked again on completion.
	if d.length >= MaxSequenceBytes {
		return
	}

	if len(d.body) == cap(d.body) {
		capacity := min(MaxSequenceBytes-3, max(128, cap(d.body)*2))
		body := make([]byte, len(d.body), capacity)
		copy(body, d.body)
		d.body = body
	}

	d.body = append(d.body, c)
}

func (d *Decoder) finishOSC(emit func(Frame)) {
	body := d.body[:len(d.body):len(d.body)]
	complete := d.length <= MaxSequenceBytes
	d.discard()

	if complete {
		emit(Frame{Kind: OSC, Body: body})
	}
}
