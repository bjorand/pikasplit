// Package host turns protocol input messages from a viewer into injected
// macOS events. The platform-specific part is behind Injector so the logic
// is testable anywhere.
package host

import (
	"math"
	"time"

	"github.com/bjorand/pikasplit/internal/keymap"
	"github.com/bjorand/pikasplit/internal/proto"
)

// Point is a position in global display coordinates (macOS points).
type Point struct{ X, Y float64 }

// Rect is a display rectangle in global coordinates.
type Rect struct{ X, Y, W, H float64 }

// Injector posts synthetic events on the Mac.
type Injector interface {
	// DisplayBounds is the display the viewer drives.
	DisplayBounds() Rect
	// MouseMove moves the cursor; held is the button state, so that moves
	// with a button down are posted as drags.
	MouseMove(p Point, held uint8)
	// MouseButton presses or releases one button (a proto.Button* bit).
	MouseButton(p Point, button uint8, down bool, clickCount int)
	// Key posts a key transition (macOS key code) with modifier flags.
	Key(vk uint16, down, repeat bool, flags uint64)
	// Scroll scrolls by pixels, macOS convention (positive = up / left).
	Scroll(dx, dy int32)
}

const (
	doubleClickTime     = 500 * time.Millisecond
	doubleClickDistance = 5.0 // points
	scrollPixelsPerStep = 40  // pixels per wheel notch (120 units)
)

type clickState struct {
	at    time.Time
	pos   Point
	count int
}

// Session holds the input state of one connected viewer.
type Session struct {
	inj  Injector
	keys *keymap.Mapper
	now  func() time.Time

	viewW, viewH int // viewer surface size; 0 means 1:1 with display points
	pos          Point
	buttons      uint8
	clicks       [8]clickState
	down         map[uint16]bool // macOS key codes currently held
	mods         keymap.ModState
	scrollRemX   int32
	scrollRemY   int32
}

// NewSession returns a session injecting through inj.
func NewSession(inj Injector, keys *keymap.Mapper) *Session {
	return &Session{inj: inj, keys: keys, now: time.Now, down: make(map[uint16]bool)}
}

// Handle applies one message and returns an optional reply.
func (s *Session) Handle(m proto.Message) proto.Message {
	switch m := m.(type) {
	case *proto.Resize:
		s.viewW, s.viewH = int(m.W), int(m.H)
	case *proto.Mouse:
		s.mouse(m)
	case *proto.Key:
		s.key(m)
	case *proto.Scroll:
		s.scroll(m)
	case *proto.Ping:
		return &proto.Pong{Timestamp: m.Timestamp}
	}
	return nil
}

func (s *Session) toDisplay(x, y int32) Point {
	b := s.inj.DisplayBounds()
	fx, fy := float64(x), float64(y)
	if s.viewW > 0 && s.viewH > 0 {
		fx = fx * b.W / float64(s.viewW)
		fy = fy * b.H / float64(s.viewH)
	}
	clamp := func(v, max float64) float64 { return math.Max(0, math.Min(v, max-1)) }
	return Point{b.X + clamp(fx, b.W), b.Y + clamp(fy, b.H)}
}

func (s *Session) mouse(m *proto.Mouse) {
	p := s.toDisplay(m.X, m.Y)
	if p != s.pos {
		s.pos = p
		s.inj.MouseMove(p, s.buttons)
	}
	for i := 0; i < 8; i++ {
		bit := uint8(1) << i
		was, is := s.buttons&bit != 0, m.Buttons&bit != 0
		if was == is {
			continue
		}
		c := &s.clicks[i]
		if is {
			now := s.now()
			if c.count > 0 && now.Sub(c.at) < doubleClickTime &&
				math.Hypot(p.X-c.pos.X, p.Y-c.pos.Y) < doubleClickDistance {
				c.count++
			} else {
				c.count = 1
			}
			c.at, c.pos = now, p
			s.buttons |= bit
		} else {
			s.buttons &^= bit
		}
		s.inj.MouseButton(p, bit, is, c.count)
	}
}

func (s *Session) key(m *proto.Key) {
	vk, ok := s.keys.Map(m.Code)
	if !ok {
		return
	}
	repeat := m.Down && s.down[vk]
	if m.Down {
		s.down[vk] = true
	} else if s.down[vk] {
		delete(s.down, vk)
	} else {
		return // release of a key we never pressed
	}
	if !repeat {
		s.mods.Update(vk, m.Down)
	}
	s.inj.Key(vk, m.Down, repeat, s.mods.Flags())
}

func (s *Session) scroll(m *proto.Scroll) {
	// Windows: positive DY = wheel away from user = scroll up (same as
	// macOS); positive DX = right (macOS: left).
	dy := int32(m.DY)*scrollPixelsPerStep + s.scrollRemY
	dx := -int32(m.DX)*scrollPixelsPerStep + s.scrollRemX
	s.scrollRemY, s.scrollRemX = dy%120, dx%120
	if dx/120 != 0 || dy/120 != 0 {
		s.inj.Scroll(dx/120, dy/120)
	}
}

// Release lifts every held key and button, e.g. when the viewer disconnects
// or loses focus, so nothing stays stuck on the Mac.
func (s *Session) Release() {
	for vk := range s.down {
		if !keymap.IsModifier(vk) {
			delete(s.down, vk)
			s.inj.Key(vk, false, false, s.mods.Flags())
		}
	}
	for vk := range s.down {
		delete(s.down, vk)
		s.mods.Update(vk, false)
		s.inj.Key(vk, false, false, s.mods.Flags())
	}
	for i := 0; i < 8; i++ {
		bit := uint8(1) << i
		if s.buttons&bit != 0 {
			s.buttons &^= bit
			s.inj.MouseButton(s.pos, bit, false, s.clicks[i].count)
		}
	}
}
