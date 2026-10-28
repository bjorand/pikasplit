// Package proto defines the wire protocol between pikasplit-host (Mac) and
// pikasplit-view (Windows).
//
// Every message is framed as:
//
//	[u32 length][u8 type][payload]
//
// where length counts the type byte plus the payload. All integers are
// big-endian.
package proto

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Version is the protocol version exchanged in Hello/Welcome.
const Version = 1

// DefaultPort is the TCP port the host listens on.
const DefaultPort = 7878

// MaxMessageSize bounds a single frame (a 4K keyframe at high bitrate fits
// comfortably).
const MaxMessageSize = 16 << 20

// Type identifies a message on the wire.
type Type uint8

const (
	TypeHello Type = iota + 1
	TypeWelcome
	TypeReject
	TypeResize
	TypeVideoFrame
	TypeRequestKeyframe
	TypeMouse
	TypeKey
	TypeScroll
	TypePing
	TypePong
)

// Codec identifies the video codec of a VideoFrame.
type Codec uint8

const (
	CodecH264 Codec = iota + 1
	CodecHEVC
)

// Mouse button bits.
const (
	ButtonLeft uint8 = 1 << iota
	ButtonRight
	ButtonMiddle
	ButtonBack
	ButtonForward
)

// Message is implemented by every protocol message.
type Message interface {
	Type() Type
	appendPayload(b []byte) []byte
	decodePayload(p []byte) error
}

// Hello is the first message sent by the viewer after the TLS handshake.
// PairPIN is only set while pairing a new viewer.
type Hello struct {
	Version uint16
	Name    string
	PairPIN string
}

// Welcome is the host's answer to an accepted Hello.
type Welcome struct {
	Version uint16
	Name    string
}

// Reject is the host's answer to a refused Hello; the host then closes.
type Reject struct {
	Reason string
}

// Resize asks the host to resize the virtual display to W x H pixels.
// Scale is the backing scale factor (1 or 2 for HiDPI).
type Resize struct {
	W, H  uint16
	Scale uint8
}

// VideoFrame carries one encoded access unit (Annex B).
type VideoFrame struct {
	Timestamp uint64 // capture time, microseconds since host start
	Keyframe  bool
	Codec     Codec
	W, H      uint16
	Data      []byte
}

// RequestKeyframe asks the host to emit an IDR frame as soon as possible.
type RequestKeyframe struct{}

// Mouse is an absolute pointer position in virtual display pixels, with the
// full button state.
type Mouse struct {
	X, Y    int32
	Buttons uint8
}

// Key is a physical key transition. Code is a USB HID usage ID (page 0x07),
// which is what SDL scancodes are.
type Key struct {
	Code uint16
	Down bool
}

// Scroll is a wheel movement in 1/120th of a notch (WHEEL_DELTA units).
type Scroll struct {
	DX, DY int16
}

// Ping is echoed by the peer as a Pong carrying the same timestamp.
type Ping struct{ Timestamp uint64 }

// Pong answers a Ping.
type Pong struct{ Timestamp uint64 }

func (*Hello) Type() Type           { return TypeHello }
func (*Welcome) Type() Type         { return TypeWelcome }
func (*Reject) Type() Type          { return TypeReject }
func (*Resize) Type() Type          { return TypeResize }
func (*VideoFrame) Type() Type      { return TypeVideoFrame }
func (*RequestKeyframe) Type() Type { return TypeRequestKeyframe }
func (*Mouse) Type() Type           { return TypeMouse }
func (*Key) Type() Type             { return TypeKey }
func (*Scroll) Type() Type          { return TypeScroll }
func (*Ping) Type() Type            { return TypePing }
func (*Pong) Type() Type            { return TypePong }

func newMessage(t Type) (Message, error) {
	switch t {
	case TypeHello:
		return &Hello{}, nil
	case TypeWelcome:
		return &Welcome{}, nil
	case TypeReject:
		return &Reject{}, nil
	case TypeResize:
		return &Resize{}, nil
	case TypeVideoFrame:
		return &VideoFrame{}, nil
	case TypeRequestKeyframe:
		return &RequestKeyframe{}, nil
	case TypeMouse:
		return &Mouse{}, nil
	case TypeKey:
		return &Key{}, nil
	case TypeScroll:
		return &Scroll{}, nil
	case TypePing:
		return &Ping{}, nil
	case TypePong:
		return &Pong{}, nil
	}
	return nil, fmt.Errorf("proto: unknown message type %d", t)
}

// Marshal returns the complete frame (header included) for m.
func Marshal(m Message) []byte {
	b := make([]byte, 5, 64)
	b[4] = byte(m.Type())
	b = m.appendPayload(b)
	binary.BigEndian.PutUint32(b, uint32(len(b)-4))
	return b
}

// Write writes m to w as a single frame.
func Write(w io.Writer, m Message) error {
	_, err := w.Write(Marshal(m))
	return err
}

// Read reads one frame from r and decodes it.
func Read(r *bufio.Reader) (Message, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > MaxMessageSize {
		return nil, fmt.Errorf("proto: invalid frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	m, err := newMessage(Type(buf[0]))
	if err != nil {
		return nil, err
	}
	if err := m.decodePayload(buf[1:]); err != nil {
		return nil, fmt.Errorf("proto: decode %T: %w", m, err)
	}
	return m, nil
}

var errShort = errors.New("short payload")

// decoder reads fields sequentially, remembering the first error.
type decoder struct {
	p   []byte
	err error
}

func (d *decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if len(d.p) < n {
		d.err = errShort
		return nil
	}
	v := d.p[:n]
	d.p = d.p[n:]
	return v
}

func (d *decoder) u8() uint8 {
	if v := d.take(1); v != nil {
		return v[0]
	}
	return 0
}

func (d *decoder) u16() uint16 {
	if v := d.take(2); v != nil {
		return binary.BigEndian.Uint16(v)
	}
	return 0
}

func (d *decoder) u32() uint32 {
	if v := d.take(4); v != nil {
		return binary.BigEndian.Uint32(v)
	}
	return 0
}

func (d *decoder) u64() uint64 {
	if v := d.take(8); v != nil {
		return binary.BigEndian.Uint64(v)
	}
	return 0
}

func (d *decoder) str() string {
	return string(d.take(int(d.u16())))
}

func (d *decoder) bytes() []byte {
	v := d.take(int(d.u32()))
	if v == nil {
		return nil
	}
	return append([]byte(nil), v...)
}

func (d *decoder) done() error {
	if d.err == nil && len(d.p) != 0 {
		return fmt.Errorf("%d trailing bytes", len(d.p))
	}
	return d.err
}

func appendStr(b []byte, s string) []byte {
	if len(s) > 0xFFFF {
		s = s[:0xFFFF]
	}
	b = binary.BigEndian.AppendUint16(b, uint16(len(s)))
	return append(b, s...)
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

func (m *Hello) appendPayload(b []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, m.Version)
	b = appendStr(b, m.Name)
	return appendStr(b, m.PairPIN)
}

func (m *Hello) decodePayload(p []byte) error {
	d := decoder{p: p}
	m.Version = d.u16()
	m.Name = d.str()
	m.PairPIN = d.str()
	return d.done()
}

func (m *Welcome) appendPayload(b []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, m.Version)
	return appendStr(b, m.Name)
}

func (m *Welcome) decodePayload(p []byte) error {
	d := decoder{p: p}
	m.Version = d.u16()
	m.Name = d.str()
	return d.done()
}

func (m *Reject) appendPayload(b []byte) []byte { return appendStr(b, m.Reason) }

func (m *Reject) decodePayload(p []byte) error {
	d := decoder{p: p}
	m.Reason = d.str()
	return d.done()
}

func (m *Resize) appendPayload(b []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, m.W)
	b = binary.BigEndian.AppendUint16(b, m.H)
	return append(b, m.Scale)
}

func (m *Resize) decodePayload(p []byte) error {
	d := decoder{p: p}
	m.W = d.u16()
	m.H = d.u16()
	m.Scale = d.u8()
	return d.done()
}

func (m *VideoFrame) appendPayload(b []byte) []byte {
	b = binary.BigEndian.AppendUint64(b, m.Timestamp)
	b = append(b, boolByte(m.Keyframe), byte(m.Codec))
	b = binary.BigEndian.AppendUint16(b, m.W)
	b = binary.BigEndian.AppendUint16(b, m.H)
	b = binary.BigEndian.AppendUint32(b, uint32(len(m.Data)))
	return append(b, m.Data...)
}

func (m *VideoFrame) decodePayload(p []byte) error {
	d := decoder{p: p}
	m.Timestamp = d.u64()
	m.Keyframe = d.u8() != 0
	m.Codec = Codec(d.u8())
	m.W = d.u16()
	m.H = d.u16()
	m.Data = d.bytes()
	return d.done()
}

func (m *RequestKeyframe) appendPayload(b []byte) []byte { return b }

func (m *RequestKeyframe) decodePayload(p []byte) error {
	d := decoder{p: p}
	return d.done()
}

func (m *Mouse) appendPayload(b []byte) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(m.X))
	b = binary.BigEndian.AppendUint32(b, uint32(m.Y))
	return append(b, m.Buttons)
}

func (m *Mouse) decodePayload(p []byte) error {
	d := decoder{p: p}
	m.X = int32(d.u32())
	m.Y = int32(d.u32())
	m.Buttons = d.u8()
	return d.done()
}

func (m *Key) appendPayload(b []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, m.Code)
	return append(b, boolByte(m.Down))
}

func (m *Key) decodePayload(p []byte) error {
	d := decoder{p: p}
	m.Code = d.u16()
	m.Down = d.u8() != 0
	return d.done()
}

func (m *Scroll) appendPayload(b []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, uint16(m.DX))
	return binary.BigEndian.AppendUint16(b, uint16(m.DY))
}

func (m *Scroll) decodePayload(p []byte) error {
	d := decoder{p: p}
	m.DX = int16(d.u16())
	m.DY = int16(d.u16())
	return d.done()
}

func (m *Ping) appendPayload(b []byte) []byte { return binary.BigEndian.AppendUint64(b, m.Timestamp) }

func (m *Ping) decodePayload(p []byte) error {
	d := decoder{p: p}
	m.Timestamp = d.u64()
	return d.done()
}

func (m *Pong) appendPayload(b []byte) []byte { return binary.BigEndian.AppendUint64(b, m.Timestamp) }

func (m *Pong) decodePayload(p []byte) error {
	d := decoder{p: p}
	m.Timestamp = d.u64()
	return d.done()
}
