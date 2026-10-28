// Package video holds codec-independent helpers for decoded frames.
package video

import (
	"runtime"
	"sync"
)

// NV12ToBGRA converts the top-left w x h pixels of an NV12 image (BT.709,
// limited range, as captured on the Mac) into dst, a top-down BGRA image
// with a stride of 4*w bytes. src holds the Y plane (yStride bytes per row,
// planeH rows) followed by the interleaved UV plane. Rows are converted in
// parallel.
func NV12ToBGRA(dst, src []byte, w, h, yStride, planeH int) {
	uv := src[yStride*planeH:]
	workers := runtime.GOMAXPROCS(0)
	pairs := (h + 1) / 2
	if workers > pairs {
		workers = pairs
	}
	var wg sync.WaitGroup
	for k := 0; k < workers; k++ {
		start, end := pairs*k/workers*2, pairs*(k+1)/workers*2
		if end > h {
			end = h
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for y := start; y < end; y++ {
				convertRow(dst[y*w*4:(y+1)*w*4], src[y*yStride:], uv[(y/2)*yStride:], w)
			}
		}()
	}
	wg.Wait()
}

func convertRow(dst, yRow, uvRow []byte, w int) {
	for x := 0; x < w; x++ {
		c := 298 * (int(yRow[x]) - 16)
		d := int(uvRow[x&^1]) - 128
		e := int(uvRow[x|1]) - 128
		o := dst[x*4 : x*4+4 : x*4+4]
		o[0] = clamp((c + 541*d + 128) >> 8)
		o[1] = clamp((c - 55*d - 136*e + 128) >> 8)
		o[2] = clamp((c + 459*e + 128) >> 8)
		o[3] = 0xFF
	}
}

func clamp(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}
