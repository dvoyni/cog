// Package ecsaudioplugin constructs the ecsaudio plugin. Only composition roots
// and tests import it; everything else reaches ecsaudio through its root.
package ecsaudioplugin

import (
	"github.com/dvoyni/cog/bundles/ecsaudio/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the ecsaudio plugin. ecsaudio has no configuration; it requires
// no Adapter.
func New() kernel.Plugin { return internal.New() }
