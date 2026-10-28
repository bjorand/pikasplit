package host

import "testing"

func TestModeFor(t *testing.T) {
	cases := []struct {
		w, h, scale, maxW, maxH int
		mw, mh                  int
		hiDPI                   bool
	}{
		{3840, 2160, 2, 3840, 2160, 1920, 1080, true},  // full screen 4K, Retina
		{3840, 2160, 1, 3840, 2160, 3840, 2160, false}, // full screen 4K, 1:1
		{2001, 1131, 2, 3840, 2160, 1000, 564, true},   // odd sizes rounded to even
		{7680, 2160, 1, 3840, 2160, 3840, 1080, false}, // too wide: aspect kept
		{200, 100, 1, 3840, 2160, 640, 360, false},     // tiny window: minimum mode
		{1600, 900, 3, 3840, 2160, 800, 450, true},     // scale capped at 2
		{1178, 644, 2, 3840, 2160, 1178, 644, false},   // too small for HiDPI: 1x
		{0, 0, 2, 3840, 2160, 1920, 1080, true},        // unknown size: max
	}
	for _, c := range cases {
		mw, mh, hi := ModeFor(c.w, c.h, c.scale, c.maxW, c.maxH)
		if mw != c.mw || mh != c.mh || hi != c.hiDPI {
			t.Errorf("ModeFor(%d, %d, %d, %d, %d) = %d, %d, %v; want %d, %d, %v",
				c.w, c.h, c.scale, c.maxW, c.maxH, mw, mh, hi, c.mw, c.mh, c.hiDPI)
		}
	}
}
