//go:build windows

package main

import (
	"log"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/bjorand/pikasplit/internal/proto"
	"github.com/bjorand/pikasplit/internal/video"
	"github.com/bjorand/pikasplit/internal/win/mf"
	"github.com/bjorand/pikasplit/internal/win/winui"
)

// videoPipeline decodes frames on a dedicated thread and hands pictures to
// the window. When it falls behind or hits a decoding error, it drops
// frames until the next keyframe and asks the host for one.
type videoPipeline struct {
	in         chan *proto.VideoFrame
	clear      chan struct{}
	resync     atomic.Bool
	requestKey func()
	onFPS      func(fps float64)
}

func newVideoPipeline(requestKey func(), onFPS func(float64)) *videoPipeline {
	p := &videoPipeline{
		in:         make(chan *proto.VideoFrame, 4),
		clear:      make(chan struct{}, 1),
		requestKey: requestKey,
		onFPS:      onFPS,
	}
	go p.run()
	return p
}

// push queues a frame without blocking the network reader.
func (p *videoPipeline) push(f *proto.VideoFrame) {
	select {
	case p.in <- f:
	default:
		if !p.resync.Swap(true) {
			p.requestKey()
		}
	}
}

// reset forgets the stream and removes the picture (connection lost).
func (p *videoPipeline) reset() {
	select {
	case p.clear <- struct{}{}:
	default:
	}
}

func (p *videoPipeline) run() {
	runtime.LockOSThread()
	if err := mf.InitThread(); err != nil {
		log.Printf("video disabled: %v", err)
		return
	}
	var (
		dec     *mf.Decoder
		waitKey = true
		buf     []byte
		frames  int
		since   = time.Now()
	)
	closeDecoder := func() {
		if dec != nil {
			dec.Close()
			dec = nil
		}
		waitKey = true
	}
	for {
		select {
		case <-p.clear:
			closeDecoder()
			buf = winui.SetFrame(nil, 0, 0)
			p.onFPS(0)
		case f := <-p.in:
			if p.resync.Swap(false) {
				waitKey = true
			}
			if waitKey {
				if !f.Keyframe {
					continue
				}
				if dec != nil {
					dec.Reset()
				}
				waitKey = false
			}
			if dec == nil {
				var err error
				if dec, err = mf.NewDecoder(); err != nil {
					log.Printf("decoder: %v", err)
					closeDecoder()
					continue
				}
			}
			err := dec.Decode(f.Data, int64(f.Timestamp), func(img mf.Image) {
				w, h := min(int(f.W), img.Width), min(int(f.H), img.Height)
				if w <= 0 || h <= 0 {
					w, h = img.Width, img.Height
				}
				if cap(buf) < w*h*4 {
					winui.FreeFrameBuffer(buf)
					buf = winui.NewFrameBuffer(w * h * 4)
				}
				buf = buf[:w*h*4]
				video.NV12ToBGRA(buf, img.Pix, w, h, img.Stride, img.PlaneH)
				buf = winui.SetFrame(buf, w, h)
				frames++
			})
			if err != nil {
				log.Printf("decode: %v", err)
				closeDecoder()
				p.requestKey()
			}
		}
		if d := time.Since(since); d >= time.Second {
			p.onFPS(float64(frames) / d.Seconds())
			frames, since = 0, time.Now()
		}
	}
}
