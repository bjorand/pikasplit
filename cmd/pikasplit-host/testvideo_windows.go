package main

import (
	"log"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/bjorand/pikasplit/internal/proto"
	"github.com/bjorand/pikasplit/internal/video"
	"github.com/bjorand/pikasplit/internal/win/mf"
)

// testVideo encodes an animated pattern with the Media Foundation encoder.
type testVideo struct {
	quit chan struct{}
	done chan struct{}
	key  atomic.Bool
}

// The software encoder cannot keep up with 60 fps at large sizes.
const maxTestFPS = 30

func startTestVideo(w, h int, opts videoOptions, sink func(*proto.VideoFrame)) (*testVideo, error) {
	fps := min(opts.fps, maxTestFPS)
	tv := &testVideo{quit: make(chan struct{}), done: make(chan struct{})}
	errc := make(chan error, 1)
	go func() {
		defer close(tv.done)
		runtime.LockOSThread()
		if err := mf.InitThread(); err != nil {
			errc <- err
			return
		}
		enc, err := mf.NewEncoder(w, h, fps, opts.bitrate)
		errc <- err
		if err != nil {
			return
		}
		defer enc.Close()
		log.Printf("test pattern %dx%d at %d fps", w, h, fps)

		pic := make([]byte, w*h*3/2)
		start := time.Now()
		tick := time.NewTicker(time.Second / time.Duration(fps))
		defer tick.Stop()
		for n := 0; ; n++ {
			select {
			case <-tv.quit:
				return
			case <-tick.C:
			}
			if tv.key.Swap(false) {
				enc.RequestKeyframe()
			}
			drawPattern(pic, w, h, n)
			ts := time.Since(start).Microseconds()
			err := enc.Encode(pic, ts, func(au []byte) {
				sink(&proto.VideoFrame{
					Timestamp: uint64(ts), Keyframe: video.IsKeyframe(au), Codec: proto.CodecH264,
					W: uint16(w), H: uint16(h), Data: append([]byte(nil), au...),
				})
			})
			if err != nil {
				log.Printf("test pattern: %v", err)
				return
			}
		}
	}()
	return tv, <-errc
}

func (tv *testVideo) requestKeyframe() { tv.key.Store(true) }

func (tv *testVideo) stop() {
	close(tv.quit)
	<-tv.done
}

// drawPattern draws scrolling diagonal stripes, a border and a box that
// bounces horizontally, so motion, scaling and cropping are easy to judge.
func drawPattern(pic []byte, w, h, n int) {
	ys, uv := pic[:w*h], pic[w*h:]
	box, boxY := w/8, h/2-h/16
	span := w - box
	bx := n * 8 % (2 * span)
	if bx > span {
		bx = 2*span - bx
	}
	for y := 0; y < h; y++ {
		row := ys[y*w : (y+1)*w]
		for x := range row {
			v := byte(60)
			if (x+y+n*4)/48%2 == 0 {
				v = 110
			}
			if x < 4 || y < 4 || x >= w-4 || y >= h-4 {
				v = 235
			}
			if x >= bx && x < bx+box && y >= boxY && y < boxY+h/8 {
				v = 200
			}
			row[x] = v
		}
	}
	for i := 0; i < len(uv); i += 2 {
		uv[i], uv[i+1] = 128, 128
		x, y := i%w, i/w*2
		if x >= bx && x < bx+box && y >= boxY && y < boxY+h/8 {
			uv[i], uv[i+1] = 90, 200 // orange-ish box
		}
	}
}
