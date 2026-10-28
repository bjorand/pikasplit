package video

// H.264 NAL unit types.
const (
	NALSlice = 1
	NALIDR   = 5
	NALSPS   = 7
	NALPPS   = 8
)

// NALTypes returns the type of each NAL unit in an Annex B buffer.
func NALTypes(au []byte) []int {
	var types []int
	for i := 0; i+3 < len(au); i++ {
		if au[i] == 0 && au[i+1] == 0 && au[i+2] == 1 {
			types = append(types, int(au[i+3]&0x1F))
			i += 3
		}
	}
	return types
}

// IsKeyframe reports whether an access unit can start decoding: it carries
// an SPS and an IDR slice.
func IsKeyframe(au []byte) bool {
	var sps, idr bool
	for _, t := range NALTypes(au) {
		sps = sps || t == NALSPS
		idr = idr || t == NALIDR
	}
	return sps && idr
}
