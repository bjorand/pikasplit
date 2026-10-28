package host

import "log"

// LogInjector logs events instead of injecting them. It lets pikasplit-host
// run on any OS for development.
type LogInjector struct {
	Bounds Rect
}

func (l *LogInjector) DisplayBounds() Rect { return l.Bounds }

func (l *LogInjector) MouseMove(p Point, held uint8) {
	log.Printf("mouse move %.0f,%.0f held=%#x", p.X, p.Y, held)
}

func (l *LogInjector) MouseButton(p Point, button uint8, down bool, clickCount int) {
	log.Printf("mouse button %#x down=%v clicks=%d at %.0f,%.0f", button, down, clickCount, p.X, p.Y)
}

func (l *LogInjector) Key(vk uint16, down, repeat bool, flags uint64) {
	log.Printf("key vk=%#x down=%v repeat=%v flags=%#x", vk, down, repeat, flags)
}

func (l *LogInjector) Scroll(dx, dy int32) { log.Printf("scroll %d,%d", dx, dy) }
