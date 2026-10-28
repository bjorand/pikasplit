//go:build !darwin

package main

import (
	"log"

	"github.com/bjorand/pikasplit/internal/host"
)

func runMain(fn func() error) error { return fn() }

func newInjector() (host.Injector, error) {
	log.Print("not on macOS: events are only logged")
	return &host.LogInjector{Bounds: host.Rect{W: 1920, H: 1080}}, nil
}

func newScreen(inj host.Injector, maxW, maxH int) screen {
	return &logScreen{inj: inj.(*host.LogInjector), maxW: maxW, maxH: maxH}
}

// logScreen pretends to resize a display, so the resize path can be tested
// off macOS.
type logScreen struct {
	inj        *host.LogInjector
	maxW, maxH int
}

func (s *logScreen) Resize(w, h, scale int) error {
	mw, mh, hiDPI := host.ModeFor(w, h, scale, s.maxW, s.maxH)
	s.inj.Bounds = host.Rect{W: float64(mw), H: float64(mh)}
	log.Printf("display: %dx%d points (HiDPI %v) for a %dx%d viewer", mw, mh, hiDPI, w, h)
	return nil
}

func (s *logScreen) Close() { log.Print("display removed") }
