//go:build windows

package mf

import (
	"runtime"
	"testing"

	"github.com/bjorand/pikasplit/internal/video"
)

// pattern fills an NV12 picture: the left half dark, the right half light,
// with neutral chroma.
func pattern(w, h int) []byte {
	pic := make([]byte, w*h*3/2)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := byte(40)
			if x >= w/2 {
				v = 200
			}
			pic[y*w+x] = v
		}
	}
	for i := w * h; i < len(pic); i++ {
		pic[i] = 128
	}
	return pic
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := InitThread(); err != nil {
		t.Fatal(err)
	}
	const w, h, fps = 320, 240, 30

	enc, err := NewEncoder(w, h, fps, 2_000_000)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	var aus [][]byte
	pic := pattern(w, h)
	for i := 0; i < 15; i++ {
		if err := enc.Encode(pic, int64(i)*1_000_000/fps, func(au []byte) {
			aus = append(aus, append([]byte(nil), au...))
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(aus) == 0 {
		t.Fatal("encoder produced nothing")
	}
	if !video.IsKeyframe(aus[0]) {
		t.Fatalf("first access unit is not a keyframe: NAL types %v", video.NALTypes(aus[0]))
	}

	dec, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	pictures := 0
	for i, au := range aus {
		if err := dec.Decode(au, int64(i)*1_000_000/fps, func(img Image) {
			pictures++
			if img.Width < w || img.Height < h || img.Stride < w {
				t.Fatalf("decoded %dx%d stride %d, want at least %dx%d", img.Width, img.Height, img.Stride, w, h)
			}
			dark, light := img.Pix[(h/2)*img.Stride+w/4], img.Pix[(h/2)*img.Stride+3*w/4]
			if dark < 30 || dark > 50 || light < 190 || light > 210 {
				t.Fatalf("luma dark=%d light=%d, want ~40 and ~200", dark, light)
			}
		}); err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
	}
	if pictures == 0 {
		t.Fatal("decoder produced no picture")
	}
	t.Logf("%d access units, %d pictures", len(aus), pictures)
}
