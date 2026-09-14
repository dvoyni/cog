// Package ecssceneplugin constructs the ecsscene plugin. Only composition roots
// and tests import it; everything else reaches ecsscene through its root.
package ecssceneplugin

import (
	"github.com/dvoyni/cog/bundles/ecsscene/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the ecsscene plugin. ecsscene has no configuration; it requires
// no Adapter.
func New() kernel.Plugin { return internal.New() }
