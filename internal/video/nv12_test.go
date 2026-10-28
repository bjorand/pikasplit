package video

import "testing"

func TestNV12ToBGRA(t *testing.T) {
	// 4x3 image, padded to a stride of 8 and a plane height of 4.
	const w, h, stride, planeH = 4, 3, 8, 4
	src := make([]byte, stride*planeH*3/2)
	y, uv := src[:stride*planeH], src[stride*planeH:]
	for i := range y {
		y[i] = 16 // black
	}
	for i := range uv {
		uv[i] = 128 // no chroma
	}
	y[0] = 235          // white at (0,0)
	y[stride+1] = 126   // mid gray at (1,1)
	uv[2], uv[3] = 0, 0 // chroma for x=2,3 rows 0-1: strong green-ish

	dst := make([]byte, w*h*4)
	NV12ToBGRA(dst, src, w, h, stride, planeH)
	px := func(x, y int) [4]byte {
		var p [4]byte
		copy(p[:], dst[(y*w+x)*4:])
		return p
	}

	if p := px(0, 0); p != [4]byte{255, 255, 255, 255} {
		t.Errorf("white = %v", p)
	}
	if p := px(3, 2); p != [4]byte{0, 0, 0, 255} {
		t.Errorf("black = %v", p)
	}
	if p := px(1, 1); p[0] != p[1] || p[1] != p[2] || p[0] < 125 || p[0] > 129 {
		t.Errorf("gray = %v", p)
	}
	// U=V=0 on black: blue and red clamp to 0, green is (55+136)*128/256.
	if p := px(2, 0); p != [4]byte{0, 96, 0, 255} {
		t.Errorf("chroma = %v", p)
	}
	// Row 2 uses the second chroma row, which is neutral.
	if p := px(2, 2); p != [4]byte{0, 0, 0, 255} {
		t.Errorf("row 2 = %v", p)
	}
}

func BenchmarkNV12ToBGRA4K(b *testing.B) {
	const w, h = 3840, 2160
	src := make([]byte, w*h*3/2)
	dst := make([]byte, w*h*4)
	b.SetBytes(int64(len(dst)))
	for i := 0; i < b.N; i++ {
		NV12ToBGRA(dst, src, w, h, w, h)
	}
}
