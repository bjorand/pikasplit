package main

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/bjorand/pikasplit/internal/darwin/inject"
	"github.com/bjorand/pikasplit/internal/darwin/mainloop"
	"github.com/bjorand/pikasplit/internal/darwin/screencast"
	"github.com/bjorand/pikasplit/internal/darwin/vdisplay"
	"github.com/bjorand/pikasplit/internal/host"
	"github.com/bjorand/pikasplit/internal/proto"
)

func runMain(fn func() error) error { return mainloop.Run(fn) }

func newInjector() (host.Injector, error) {
	inj, err := inject.New()
	if errors.Is(err, inject.ErrNotTrusted) {
		return nil, fmt.Errorf("%w\nAllow the app running pikasplit-host (Terminal, iTerm…) in\n"+
			"System Settings > Privacy & Security > Accessibility, then restart", err)
	}
	return inj, err
}

func newScreen(inj host.Injector, maxW, maxH int, video videoOptions) screen {
	return &virtualScreen{inj: inj.(*inject.Injector), maxW: maxW, maxH: maxH, video: video}
}

// virtualScreen is a virtual display sized to the viewer window, streamed
// to the viewer. It exists while a viewer is connected; input goes back to
// the main display after.
type virtualScreen struct {
	inj        *inject.Injector
	maxW, maxH int
	video      videoOptions

	mu     sync.Mutex
	vd     *vdisplay.Display
	stream *screencast.Stream
}

func (s *virtualScreen) Resize(w, h, scale int, sink func(*proto.VideoFrame)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mw, mh, hiDPI := host.ModeFor(w, h, scale, s.maxW, s.maxH)
	if s.vd == nil {
		vd, err := vdisplay.New("pikasplit", s.maxW, s.maxH)
		if err != nil {
			return err
		}
		s.vd = vd
		log.Printf("virtual display %d created", vd.ID())
	}
	if err := s.vd.SetMode(mw, mh, hiDPI); err != nil {
		return err
	}
	s.inj.SetDisplay(s.vd.ID())
	log.Printf("virtual display %d: %dx%d points (HiDPI %v) for a %dx%d viewer", s.vd.ID(), mw, mh, hiDPI, w, h)

	s.stopStream()
	pw, ph := mw, mh
	if hiDPI {
		pw, ph = 2*mw, 2*mh
	}
	stream, err := screencast.Start(screencast.Config{
		DisplayID: s.vd.ID(), Width: pw, Height: ph,
		FPS: s.video.fps, Bitrate: s.video.bitrate,
	}, func(f screencast.Frame) {
		sink(&proto.VideoFrame{
			Timestamp: uint64(f.PTS), Keyframe: f.Keyframe, Codec: proto.CodecH264,
			W: uint16(pw), H: uint16(ph), Data: f.Data,
		})
	}, func(err error) {
		log.Print(err)
	})
	if err != nil {
		return err
	}
	s.stream = stream
	log.Printf("streaming %dx%d at %d fps, %d Mbit/s", pw, ph, s.video.fps, s.video.bitrate/1_000_000)
	return nil
}

func (s *virtualScreen) RequestKeyframe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stream != nil {
		s.stream.RequestKeyframe()
	}
}

func (s *virtualScreen) stopStream() {
	if s.stream != nil {
		s.stream.Stop()
		s.stream = nil
	}
}

func (s *virtualScreen) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopStream()
	if s.vd == nil {
		return
	}
	s.inj.SetDisplay(inject.MainDisplayID())
	log.Printf("virtual display %d removed", s.vd.ID())
	s.vd.Close()
	s.vd = nil
}
