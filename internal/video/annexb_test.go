package video

import (
	"reflect"
	"testing"
)

func TestNALTypes(t *testing.T) {
	key := []byte{0, 0, 0, 1, 0x67, 1, 2, 0, 0, 0, 1, 0x68, 3, 0, 0, 1, 0x65, 4, 5}
	if got := NALTypes(key); !reflect.DeepEqual(got, []int{NALSPS, NALPPS, NALIDR}) {
		t.Fatalf("NALTypes = %v", got)
	}
	if !IsKeyframe(key) {
		t.Fatal("keyframe not detected")
	}
	delta := []byte{0, 0, 0, 1, 0x41, 9, 9}
	if IsKeyframe(delta) || !reflect.DeepEqual(NALTypes(delta), []int{NALSlice}) {
		t.Fatal("delta frame misdetected")
	}
	if NALTypes(nil) != nil {
		t.Fatal("empty input")
	}
}
