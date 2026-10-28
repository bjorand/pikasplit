package host

import "math"

// Smallest virtual display mode we create, in points.
const (
	minModeW = 640
	minModeH = 360
)

// ModeFor returns the virtual display mode (in points) matching a viewer
// surface of w x h pixels. With scale 2 the display is HiDPI: each point is
// backed by 2x2 pixels, so text stays sharp and readable on a 4K monitor.
// The backing size is kept within maxW x maxH pixels, preserving the aspect
// ratio. A window too small for a HiDPI mode gets a 1x mode rather than a
// distorted one. Dimensions are even, as display modes require.
func ModeFor(w, h, scale, maxW, maxH int) (modeW, modeH int, hiDPI bool) {
	if scale >= 2 {
		scale = 2
	} else {
		scale = 1
	}
	if w <= 0 || h <= 0 {
		w, h = maxW, maxH
	}
	f := math.Min(1, math.Min(float64(maxW)/float64(w), float64(maxH)/float64(h)))
	if scale == 2 && (float64(w)*f/2 < minModeW || float64(h)*f/2 < minModeH) {
		scale = 1
	}
	even := func(v float64, min int) int {
		n := int(v) &^ 1
		if n < min {
			n = min
		}
		return n
	}
	modeW = even(float64(w)*f/float64(scale), minModeW)
	modeH = even(float64(h)*f/float64(scale), minModeH)
	return modeW, modeH, scale == 2
}
