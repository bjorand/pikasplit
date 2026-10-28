package host

import (
	"testing"

	"github.com/bjorand/pikasplit/internal/proto"
)

func TestFrameQueue(t *testing.T) {
	requests := 0
	q := NewFrameQueue(2, func() { requests++ })
	frame := func(ts uint64, key bool) *proto.VideoFrame {
		return &proto.VideoFrame{Timestamp: ts, Keyframe: key}
	}
	drain := func() []uint64 {
		var got []uint64
		for len(q.C) > 0 {
			got = append(got, (<-q.C).Timestamp)
		}
		return got
	}

	q.Push(frame(1, false)) // no keyframe yet: dropped
	q.Push(frame(2, true))
	q.Push(frame(3, false))
	q.Push(frame(4, false)) // full: dropped, keyframe requested
	q.Push(frame(5, false)) // waiting for a keyframe: dropped
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if got := drain(); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("got %v, want [2 3]", got)
	}
	q.Push(frame(6, false))
	q.Push(frame(7, true))
	q.Push(frame(8, false))
	if got := drain(); len(got) != 2 || got[0] != 7 || got[1] != 8 {
		t.Fatalf("got %v, want [7 8]", got)
	}

	q.Reset()
	q.Push(frame(9, false))
	if requests != 2 || len(q.C) != 0 {
		t.Fatalf("after reset: requests = %d, queued = %d", requests, len(q.C))
	}

	q.Close()
	q.Push(frame(10, true))
	if _, ok := <-q.C; ok {
		t.Fatal("frame queued after close")
	}
}
