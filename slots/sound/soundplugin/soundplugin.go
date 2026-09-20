// Package soundplugin constructs the sound plugin. Only composition roots and
// tests import it; everything else reaches sound through its root.
package soundplugin

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/sound/internal"
)

// New creates the sound plugin. Its sound.Config arrives through kernel.New's
// config map under sound.Name, and it requires exactly one Adapter for
// sound.BackendPort.
func New() kernel.Plugin { return internal.New() }
