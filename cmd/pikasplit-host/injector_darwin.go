package main

import (
	"errors"
	"fmt"

	"github.com/bjorand/pikasplit/internal/darwin/inject"
	"github.com/bjorand/pikasplit/internal/host"
)

func newInjector() (host.Injector, error) {
	inj, err := inject.New()
	if errors.Is(err, inject.ErrNotTrusted) {
		return nil, fmt.Errorf("%w\nAllow the app running pikasplit-host (Terminal, iTerm…) in\n"+
			"System Settings > Privacy & Security > Accessibility, then restart", err)
	}
	return inj, err
}
