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
		return nil, fmt.Errorf("%w\nAutorisez l'app qui lance pikasplit-host (Terminal, iTerm…) dans\n"+
			"Réglages Système > Confidentialité et sécurité > Accessibilité, puis relancez", err)
	}
	return inj, err
}
