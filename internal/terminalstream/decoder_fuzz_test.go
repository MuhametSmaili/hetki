package terminalstream

import (
	"reflect"
	"testing"
)

func FuzzDecoderChunkBoundaries(f *testing.F) {
	f.Add([]byte("\x1b]7501;state=working\a\x1b(c\x1bc"), uint8(1))
	f.Add([]byte("\x1bPtmux;\x1b\x1b]7501;?\x1b\x1b\\\x1b\\"), uint8(3))
	f.Add([]byte("\x1b]7501;state=idle\x1b\x18\x1b]133;A\a"), uint8(5))
	f.Add([]byte("text 🌍\xc2\x9d\x1b[!p"), uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, stride uint8) {
		if len(data) > 1<<16 {
			t.Skip()
		}

		want := collect(string(data))
		var decoder Decoder
		var got []observation
		emit := func(frame Frame) {
			if frame.Kind == OSC && len(frame.Body) > MaxSequenceBytes-3 {
				t.Fatal("emitted body exceeds the sequence bound")
			}
			got = append(got, observation{kind: frame.Kind, body: string(frame.Body)})
		}
		step := int(stride) + 1

		for offset := 0; offset < len(data); offset += step {
			decoder.Feed(data[offset:min(offset+step, len(data))], emit)
		}

		if !reflect.DeepEqual(want, got) {
			t.Fatal("framing depends on input chunk boundaries")
		}
		if cap(decoder.body) > MaxSequenceBytes-3 || decoder.length > MaxSequenceBytes+1 {
			t.Fatal("decoder retained unbounded state")
		}
	})
}
