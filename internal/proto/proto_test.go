package proto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	msgs := []Message{
		&Hello{Version: Version, Name: "pc-salon", PairPIN: "123456"},
		&Welcome{Version: Version, Name: "macbook"},
		&Reject{Reason: "not paired"},
		&Resize{W: 2560, H: 1440, Scale: 2},
		&VideoFrame{Timestamp: 1 << 40, Keyframe: true, Codec: CodecH264, W: 1920, H: 1080, Data: []byte{0, 0, 0, 1, 0x67}},
		&VideoFrame{Codec: CodecHEVC, Data: []byte{}},
		&RequestKeyframe{},
		&Mouse{X: -12, Y: 2159, Buttons: ButtonLeft | ButtonMiddle},
		&Key{Code: 0x04, Down: true},
		&Scroll{DX: -120, DY: 240},
		&Ping{Timestamp: 42},
		&Pong{Timestamp: 43},
	}

	var buf bytes.Buffer
	for _, m := range msgs {
		if err := Write(&buf, m); err != nil {
			t.Fatal(err)
		}
	}
	r := bufio.NewReader(&buf)
	for _, want := range msgs {
		got, err := Read(r)
		if err != nil {
			t.Fatalf("read %T: %v", want, err)
		}
		if vf, ok := want.(*VideoFrame); ok && len(vf.Data) == 0 {
			// Empty and nil data are equivalent on the wire.
			got.(*VideoFrame).Data = vf.Data
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %#v, want %#v", got, want)
		}
	}
	if buf.Len() != 0 {
		t.Errorf("%d bytes left over", buf.Len())
	}
}

func TestReadRejectsBadFrames(t *testing.T) {
	frame := func(typ byte, payload ...byte) []byte {
		b := binary.BigEndian.AppendUint32(nil, uint32(1+len(payload)))
		return append(append(b, typ), payload...)
	}
	cases := map[string][]byte{
		"zero length":   {0, 0, 0, 0},
		"too large":     binary.BigEndian.AppendUint32(nil, MaxMessageSize+1),
		"unknown type":  frame(0xFF),
		"short payload": frame(byte(TypeResize), 0, 1),
		"trailing":      frame(byte(TypePing), 0, 0, 0, 0, 0, 0, 0, 0, 9),
		"truncated":     frame(byte(TypePing), 0, 0)[:4],
	}
	for name, b := range cases {
		if _, err := Read(bufio.NewReader(bytes.NewReader(b))); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
