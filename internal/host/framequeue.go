package host

import (
	"sync"

	"github.com/bjorand/pikasplit/internal/proto"
)

// FrameQueue sits between the encoder and a slow network: it never blocks
// the encoder. When the queue is full a frame is dropped, and since later
// frames depend on it, everything is dropped until the next keyframe, which
// is requested right away.
type FrameQueue struct {
	C <-chan *proto.VideoFrame

	ch         chan *proto.VideoFrame
	requestKey func()

	mu      sync.Mutex
	needKey bool
	closed  bool
}

// NewFrameQueue returns a queue holding up to size frames. requestKey asks
// the encoder for a keyframe; it must not block.
func NewFrameQueue(size int, requestKey func()) *FrameQueue {
	ch := make(chan *proto.VideoFrame, size)
	return &FrameQueue{C: ch, ch: ch, requestKey: requestKey, needKey: true}
}

// Push queues f, or drops it.
func (q *FrameQueue) Push(f *proto.VideoFrame) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	if q.needKey && !f.Keyframe {
		return
	}
	select {
	case q.ch <- f:
		q.needKey = false
	default:
		if !q.needKey {
			q.needKey = true
			q.requestKey()
		}
	}
}

// Reset drops frames until the next keyframe and requests one, e.g. when
// the viewer reports it lost track of the stream.
func (q *FrameQueue) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.needKey = true
	q.requestKey()
}

// Close closes C; later pushes are ignored.
func (q *FrameQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.ch)
	}
}
