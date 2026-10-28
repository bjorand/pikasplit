//go:build !darwin

package main

import (
	"log"
	"sync"

	"github.com/bjorand/pikasplit/internal/host"
	"github.com/bjorand/pikasplit/internal/proto"
)

func runMain(fn func() error) error { return fn() }

func newInjector() (host.Injector, error) {
	log.Print("not on macOS: events are only logged, video is a test pattern")
	return &host.LogInjector{Bounds: host.Rect{W: 1920, H: 1080}}, nil
}

func newScreen(inj host.Injector, maxW, maxH int, video videoOptions) screen {
	return &logScreen{inj: inj.(*host.LogInjector), maxW: maxW, maxH: maxH, video: video}
}

// logScreen pretends to resize a display and streams a test pattern, so the
// viewer can be developed without a Mac.
type logScreen struct {
	inj        *host.LogInjector
	maxW, maxH int
	video      videoOptions

	mu     sync.Mutex
	source *testVideo
}

func (s *logScreen) Resize(w, h, scale int, sink func(*proto.VideoFrame)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mw, mh, hiDPI := host.ModeFor(w, h, scale, s.maxW, s.maxH)
	s.inj.Bounds = host.Rect{W: float64(mw), H: float64(mh)}
	log.Printf("display: %dx%d points (HiDPI %v) for a %dx%d viewer", mw, mh, hiDPI, w, h)

	s.stop()
	pw, ph := mw, mh
	if hiDPI {
		pw, ph = 2*mw, 2*mh
	}
	src, err := startTestVideo(pw, ph, s.video, sink)
	if err != nil {
		return err
	}
	s.source = src
	return nil
}

func (s *logScreen) RequestKeyframe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.source != nil {
		s.source.requestKeyframe()
	}
}

func (s *logScreen) stop() {
	if s.source != nil {
		s.source.stop()
		s.source = nil
	}
}

func (s *logScreen) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop()
	log.Print("display removed")
}
