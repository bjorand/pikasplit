//go:build !darwin && !windows

package main

import (
	"errors"

	"github.com/bjorand/pikasplit/internal/proto"
)

type testVideo struct{}

func startTestVideo(w, h int, opts videoOptions, sink func(*proto.VideoFrame)) (*testVideo, error) {
	return nil, errors.New("no test video source on this OS")
}

func (*testVideo) requestKeyframe() {}
func (*testVideo) stop()            {}
