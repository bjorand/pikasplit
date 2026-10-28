package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/bjorand/pikasplit/internal/darwin/inject"
	"github.com/bjorand/pikasplit/internal/darwin/mainloop"
	"github.com/bjorand/pikasplit/internal/darwin/vdisplay"
	"github.com/bjorand/pikasplit/internal/host"
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

func newScreen(inj host.Injector, maxW, maxH int) screen {
	return &virtualScreen{inj: inj.(*inject.Injector), maxW: maxW, maxH: maxH}
}

// virtualScreen is a virtual display sized to the viewer window. It exists
// while a viewer is connected; input goes back to the main display after.
type virtualScreen struct {
	inj        *inject.Injector
	maxW, maxH int
	vd         *vdisplay.Display
}

func (s *virtualScreen) Resize(w, h, scale int) error {
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
	return nil
}

func (s *virtualScreen) Close() {
	if s.vd == nil {
		return
	}
	s.inj.SetDisplay(inject.MainDisplayID())
	log.Printf("virtual display %d removed", s.vd.ID())
	s.vd.Close()
	s.vd = nil
}
