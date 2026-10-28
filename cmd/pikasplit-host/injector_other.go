//go:build !darwin

package main

import (
	"log"

	"github.com/bjorand/pikasplit/internal/host"
)

func newInjector() (host.Injector, error) {
	log.Print("not on macOS: events are only logged")
	return &host.LogInjector{Bounds: host.Rect{W: 1920, H: 1080}}, nil
}
