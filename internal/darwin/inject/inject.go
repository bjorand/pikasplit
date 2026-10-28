//go:build darwin

// Package inject posts synthetic mouse and keyboard events on macOS through
// CoreGraphics. The process needs the Accessibility permission (System
// Settings > Privacy & Security > Accessibility); when launched from a
// terminal, it is the terminal app that must be allowed.
package inject

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

static CGEventSourceRef src;

static void psInit(void) {
	src = CGEventSourceCreate(kCGEventSourceStateCombinedSessionState);
	// Do not freeze the local mouse/keyboard after each injected event.
	CGEventSourceSetLocalEventsSuppressionInterval(src, 0.0);
}

static int psTrusted(int prompt) {
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *vals[] = { prompt ? kCFBooleanTrue : kCFBooleanFalse };
	CFDictionaryRef opts = CFDictionaryCreate(NULL, keys, vals, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	Boolean ok = AXIsProcessTrustedWithOptions(opts);
	CFRelease(opts);
	return ok;
}

static void psMouse(CGEventType type, double x, double y, int button, int clicks, uint64_t flags) {
	CGEventRef e = CGEventCreateMouseEvent(src, type, CGPointMake(x, y), (CGMouseButton)button);
	if (e == NULL) return;
	if (clicks > 0) CGEventSetIntegerValueField(e, kCGMouseEventClickState, clicks);
	CGEventSetFlags(e, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, e);
	CFRelease(e);
}

static void psKey(int vk, int down, int repeat, uint64_t flags) {
	CGEventRef e = CGEventCreateKeyboardEvent(src, (CGKeyCode)vk, down ? true : false);
	if (e == NULL) return;
	CGEventSetFlags(e, (CGEventFlags)flags);
	if (repeat) CGEventSetIntegerValueField(e, kCGKeyboardEventAutorepeat, 1);
	CGEventPost(kCGHIDEventTap, e);
	CFRelease(e);
}

// CGEventCreateScrollWheelEvent is variadic, which cgo cannot call directly.
static void psScroll(int32_t dx, int32_t dy, uint64_t flags) {
	CGEventRef e = CGEventCreateScrollWheelEvent(src, kCGScrollEventUnitPixel, 2, dy, dx);
	if (e == NULL) return;
	CGEventSetFlags(e, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, e);
	CFRelease(e);
}
*/
import "C"

import (
	"errors"
	"sync"

	"github.com/bjorand/pikasplit/internal/host"
	"github.com/bjorand/pikasplit/internal/proto"
)

var initOnce sync.Once

// ErrNotTrusted means the Accessibility permission is missing.
var ErrNotTrusted = errors.New("inject: Accessibility permission not granted")

// Trusted reports whether the process may post events. With prompt, macOS
// shows its permission dialog if needed.
func Trusted(prompt bool) bool {
	p := C.int(0)
	if prompt {
		p = 1
	}
	return C.psTrusted(p) != 0
}

// Injector implements host.Injector for one display.
type Injector struct {
	display C.CGDirectDisplayID
	flags   uint64 // last modifier flags, applied to mouse and scroll events
}

var _ host.Injector = (*Injector)(nil)

// New returns an injector targeting the main display.
func New() (*Injector, error) {
	if !Trusted(true) {
		return nil, ErrNotTrusted
	}
	initOnce.Do(func() { C.psInit() })
	return &Injector{display: C.CGMainDisplayID()}, nil
}

// SetDisplay changes the target display (e.g. the virtual display).
func (i *Injector) SetDisplay(id uint32) { i.display = C.CGDirectDisplayID(id) }

// DisplayBounds implements host.Injector.
func (i *Injector) DisplayBounds() host.Rect {
	r := C.CGDisplayBounds(i.display)
	return host.Rect{
		X: float64(r.origin.x), Y: float64(r.origin.y),
		W: float64(r.size.width), H: float64(r.size.height),
	}
}

// cgButton maps a proto button bit to its CoreGraphics button number and
// down/up/dragged event types.
func cgButton(bit uint8) (num int, down, up, drag C.CGEventType) {
	switch bit {
	case proto.ButtonLeft:
		return 0, C.kCGEventLeftMouseDown, C.kCGEventLeftMouseUp, C.kCGEventLeftMouseDragged
	case proto.ButtonRight:
		return 1, C.kCGEventRightMouseDown, C.kCGEventRightMouseUp, C.kCGEventRightMouseDragged
	case proto.ButtonMiddle:
		num = 2
	case proto.ButtonBack:
		num = 3
	case proto.ButtonForward:
		num = 4
	default:
		num = 5
	}
	return num, C.kCGEventOtherMouseDown, C.kCGEventOtherMouseUp, C.kCGEventOtherMouseDragged
}

// MouseMove implements host.Injector.
func (i *Injector) MouseMove(p host.Point, held uint8) {
	typ, num := C.CGEventType(C.kCGEventMouseMoved), 0
	for _, bit := range []uint8{proto.ButtonLeft, proto.ButtonRight, proto.ButtonMiddle, proto.ButtonBack, proto.ButtonForward} {
		if held&bit != 0 {
			var drag C.CGEventType
			num, _, _, drag = cgButton(bit)
			typ = drag
			break
		}
	}
	C.psMouse(typ, C.double(p.X), C.double(p.Y), C.int(num), 0, C.uint64_t(i.flags))
}

// MouseButton implements host.Injector.
func (i *Injector) MouseButton(p host.Point, button uint8, down bool, clickCount int) {
	num, d, u, _ := cgButton(button)
	typ := u
	if down {
		typ = d
	}
	C.psMouse(typ, C.double(p.X), C.double(p.Y), C.int(num), C.int(clickCount), C.uint64_t(i.flags))
}

// Key implements host.Injector.
func (i *Injector) Key(vk uint16, down, repeat bool, flags uint64) {
	i.flags = flags
	b := func(v bool) C.int {
		if v {
			return 1
		}
		return 0
	}
	C.psKey(C.int(vk), b(down), b(repeat), C.uint64_t(flags))
}

// Scroll implements host.Injector.
func (i *Injector) Scroll(dx, dy int32) {
	C.psScroll(C.int32_t(dx), C.int32_t(dy), C.uint64_t(i.flags))
}
