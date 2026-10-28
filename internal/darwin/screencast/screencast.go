//go:build darwin

// Package screencast captures a display with ScreenCaptureKit and encodes it
// to H.264 with the hardware encoder (VideoToolbox, low-latency rate
// control). Frames stay in native code until compressed. The process needs
// the Screen Recording permission.
package screencast

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework CoreMedia -framework CoreVideo -framework VideoToolbox -framework ScreenCaptureKit
#include <stdlib.h>
#include "screencast.h"
*/
import "C"

import (
	"errors"
	"runtime/cgo"
	"unsafe"
)

// Frame is one encoded access unit in Annex B format.
type Frame struct {
	Data     []byte
	PTS      int64 // microseconds, host clock
	Keyframe bool
}

// Config describes a capture.
type Config struct {
	DisplayID     uint32
	Width, Height int // pixels
	FPS           int
	Bitrate       int // bits per second
}

type callbacks struct {
	frame func(Frame)
	err   func(error)
}

// Stream is a running capture.
type Stream struct {
	p      unsafe.Pointer
	handle cgo.Handle
}

// Start begins capturing. onFrame and onError run on internal threads and
// must not block for long; onError reports that the capture stopped.
func Start(cfg Config, onFrame func(Frame), onError func(error)) (*Stream, error) {
	h := cgo.NewHandle(&callbacks{frame: onFrame, err: onError})
	var cerr *C.char
	p := C.scStart(C.uint32_t(cfg.DisplayID), C.int(cfg.Width), C.int(cfg.Height),
		C.int(cfg.FPS), C.int(cfg.Bitrate), C.uintptr_t(h), &cerr)
	if p == nil {
		h.Delete()
		msg := "screencast: start failed"
		if cerr != nil {
			msg = "screencast: " + C.GoString(cerr)
			C.free(unsafe.Pointer(cerr))
		}
		return nil, errors.New(msg)
	}
	return &Stream{p: p, handle: h}, nil
}

// RequestKeyframe makes the next frame a keyframe.
func (s *Stream) RequestKeyframe() { C.scRequestKeyframe(s.p) }

// Stop ends the capture; no callback runs after it returns.
func (s *Stream) Stop() {
	if s.p == nil {
		return
	}
	C.scStop(s.p)
	s.p = nil
	s.handle.Delete()
}

//export goScFrame
func goScFrame(handle C.uintptr_t, data *C.uint8_t, n C.int, pts C.int64_t, key C.int) {
	cb := cgo.Handle(handle).Value().(*callbacks)
	cb.frame(Frame{Data: C.GoBytes(unsafe.Pointer(data), n), PTS: int64(pts), Keyframe: key != 0})
}

//export goScError
func goScError(handle C.uintptr_t, msg *C.char) {
	cb := cgo.Handle(handle).Value().(*callbacks)
	cb.err(errors.New("screencast: " + C.GoString(msg)))
}
