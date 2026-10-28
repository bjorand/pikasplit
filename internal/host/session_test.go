package host

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bjorand/pikasplit/internal/keymap"
	"github.com/bjorand/pikasplit/internal/proto"
)

type fakeInjector struct {
	bounds Rect
	calls  []string
}

func (f *fakeInjector) DisplayBounds() Rect { return f.bounds }
func (f *fakeInjector) MouseMove(p Point, held uint8) {
	f.calls = append(f.calls, fmt.Sprintf("move %g,%g held=%d", p.X, p.Y, held))
}
func (f *fakeInjector) MouseButton(p Point, b uint8, down bool, n int) {
	f.calls = append(f.calls, fmt.Sprintf("button %d down=%v n=%d at %g,%g", b, down, n, p.X, p.Y))
}
func (f *fakeInjector) Key(vk uint16, down, repeat bool, flags uint64) {
	f.calls = append(f.calls, fmt.Sprintf("key %#x down=%v repeat=%v flags=%#x", vk, down, repeat, flags))
}
func (f *fakeInjector) Scroll(dx, dy int32) {
	f.calls = append(f.calls, fmt.Sprintf("scroll %d,%d", dx, dy))
}

func (f *fakeInjector) take() []string {
	c := f.calls
	f.calls = nil
	return c
}

func newTestSession() (*Session, *fakeInjector, *time.Time) {
	inj := &fakeInjector{bounds: Rect{X: 1000, Y: 0, W: 1920, H: 1080}}
	s := NewSession(inj, keymap.New(keymap.Options{}))
	now := time.Unix(0, 0)
	s.now = func() time.Time { return now }
	return s, inj, &now
}

func expect(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("calls:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestMouseScalingAndDrag(t *testing.T) {
	s, inj, _ := newTestSession()
	s.Handle(&proto.Resize{W: 3840, H: 2160})
	s.Handle(&proto.Mouse{X: 200, Y: 100})
	s.Handle(&proto.Mouse{X: 200, Y: 100, Buttons: proto.ButtonLeft})
	s.Handle(&proto.Mouse{X: 400, Y: 100, Buttons: proto.ButtonLeft})
	s.Handle(&proto.Mouse{X: 400, Y: 100})
	s.Handle(&proto.Mouse{X: 99999, Y: -5}) // clamped to the display
	expect(t, inj.take(),
		"move 1100,50 held=0",
		"button 1 down=true n=1 at 1100,50",
		"move 1200,50 held=1",
		"button 1 down=false n=1 at 1200,50",
		"move 2919,0 held=0",
	)
}

func TestDoubleClick(t *testing.T) {
	s, inj, now := newTestSession()
	click := func() {
		s.Handle(&proto.Mouse{X: 10, Y: 10, Buttons: proto.ButtonLeft})
		s.Handle(&proto.Mouse{X: 10, Y: 10})
	}
	click()
	*now = now.Add(200 * time.Millisecond)
	click()
	*now = now.Add(time.Second)
	click()
	expect(t, inj.take(),
		"move 1010,10 held=0",
		"button 1 down=true n=1 at 1010,10",
		"button 1 down=false n=1 at 1010,10",
		"button 1 down=true n=2 at 1010,10",
		"button 1 down=false n=2 at 1010,10",
		"button 1 down=true n=1 at 1010,10",
		"button 1 down=false n=1 at 1010,10",
	)
}

func TestKeysModifiersAndRepeat(t *testing.T) {
	s, inj, _ := newTestSession()
	s.Handle(&proto.Key{Code: 0xE3, Down: true}) // Win -> Command
	s.Handle(&proto.Key{Code: 0x06, Down: true}) // c
	s.Handle(&proto.Key{Code: 0x06, Down: true}) // auto-repeat
	s.Handle(&proto.Key{Code: 0x06, Down: false})
	s.Handle(&proto.Key{Code: 0xE3, Down: false})
	s.Handle(&proto.Key{Code: 0x07, Down: false}) // stray release ignored
	s.Handle(&proto.Key{Code: 0x65, Down: true})  // unmapped ignored
	expect(t, inj.take(),
		"key 0x37 down=true repeat=false flags=0x100000",
		"key 0x8 down=true repeat=false flags=0x100000",
		"key 0x8 down=true repeat=true flags=0x100000",
		"key 0x8 down=false repeat=false flags=0x100000",
		"key 0x37 down=false repeat=false flags=0x0",
	)
}

func TestScroll(t *testing.T) {
	s, inj, _ := newTestSession()
	s.Handle(&proto.Scroll{DY: 120})
	s.Handle(&proto.Scroll{DX: 120})
	// High-resolution deltas accumulate until they amount to a pixel.
	s.Handle(&proto.Scroll{DY: 2})
	s.Handle(&proto.Scroll{DY: 2})
	expect(t, inj.take(), "scroll 0,40", "scroll -40,0", "scroll 0,1")
}

func TestReleaseLiftsEverything(t *testing.T) {
	s, inj, _ := newTestSession()
	s.Handle(&proto.Key{Code: 0xE1, Down: true}) // Shift
	s.Handle(&proto.Key{Code: 0x04, Down: true}) // a
	s.Handle(&proto.Mouse{X: 1, Y: 1, Buttons: proto.ButtonRight})
	inj.take()
	s.Release()
	expect(t, inj.take(),
		"key 0x0 down=false repeat=false flags=0x20000",
		"key 0x38 down=false repeat=false flags=0x0",
		"button 2 down=false n=1 at 1001,1",
	)
	s.Release()
	expect(t, inj.take())
}

func TestPingPong(t *testing.T) {
	s, _, _ := newTestSession()
	if r, ok := s.Handle(&proto.Ping{Timestamp: 9}).(*proto.Pong); !ok || r.Timestamp != 9 {
		t.Fatalf("reply = %#v", r)
	}
}
